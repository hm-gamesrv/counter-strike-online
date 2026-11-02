package server

import (
	"context"
	"crypto/rc4"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

type outbound struct {
	id    byte
	data  []byte
	crypt *rc4.Cipher
}
type client struct {
	conn                             net.Conn
	codec                            *protocol.Codec
	queue                            chan outbound
	user                             store.User
	character                        store.Character
	hwid                             []byte
	version, loginHandshake, inLobby bool
	cryptIn                          *rc4.Cipher
	dedicated                        *dedicatedServer
	room, team, ready                int
	playing                          bool
	questLogin                       bool
	localIP                          net.IP
	localPorts, externalPorts        [2]int
}

func (c *client) ip() string { h, _, _ := net.SplitHostPort(c.conn.RemoteAddr().String()); return h }
func (c *client) send(id byte, w *protocol.Writer) {
	data := append([]byte{}, w.Data...)
	select {
	case c.queue <- outbound{id: id, data: data}:
	default:
		c.conn.Close()
	}
}
func (c *client) notice(text string) {
	w := &protocol.Writer{}
	w.U8(10)
	w.String(text)
	c.send(67, w)
}
func (c *client) chatNotice(text string) {
	w := &protocol.Writer{}
	w.U8(11)
	w.String(text)
	c.send(67, w)
}
func (c *client) reply(code int) {
	w := &protocol.Writer{}
	w.U8(code)
	w.String("S_REPLY_OK")
	w.U8(0)
	c.send(1, w)
}

type Server struct {
	Config       config.Config
	Store        *store.Store
	Log          *slog.Logger
	metadata     [][]byte
	shop         shop
	items        map[int]itemDefinition
	itemBoxes    map[int]*itemBox
	weaponPaints map[int][]int
	rewards      map[int]itemReward
	quests       []questTask
	surveys      []surveyDefinition
	enhanceExp   map[int][]int
	enhanceMax   map[int]enhanceLimit
	modes        map[int]modeDefinition
	dedicated    []*dedicatedServer
	mu           sync.Mutex
	clients      map[*client]bool
	users        map[int]*client
	rooms        map[int]*room
	nextRoom     int
}

func New(dir string, c config.Config, db *store.Store, log *slog.Logger) (*Server, error) {
	if err := validateWireLimits(c); err != nil {
		return nil, err
	}
	if _, err := defaultInventoryPacket(c.DefaultUser.DefaultItems); err != nil {
		return nil, err
	}
	m, e := loadMetadata(dir, c)
	if e != nil {
		return nil, e
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Server{Config: c, Store: db, Log: log, metadata: m, clients: map[*client]bool{}, users: map[int]*client{}, rooms: map[int]*room{}, nextRoom: 1}
	s.shop, e = loadShop(dir)
	if e != nil {
		return nil, e
	}
	s.items, e = loadItems(dir)
	if e != nil {
		return nil, e
	}
	s.itemBoxes, e = loadItemBoxes(dir)
	if e != nil {
		return nil, e
	}
	s.weaponPaints, e = loadWeaponPaints(dir)
	if e != nil {
		return nil, e
	}
	s.modes, e = loadModes(dir)
	if e != nil {
		return nil, e
	}
	s.rewards, e = loadRewards(dir)
	if e != nil {
		return nil, e
	}
	s.quests, e = loadEventQuests(dir)
	if e != nil {
		return nil, e
	}
	s.surveys, e = loadSurveys(c.Surveys)
	if e != nil {
		return nil, e
	}
	s.enhanceExp, e = loadReinforceExp(dir)
	if e != nil {
		return nil, e
	}
	s.enhanceMax, e = loadReinforceMax(dir)
	if e != nil {
		return nil, e
	}
	s.logQuestWarnings()
	return s, nil
}

// Serve owns listeners and all accepted connections until context cancellation.
func (s *Server) Serve(ctx context.Context, tcp net.Listener, udp *net.UDPConn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer tcp.Close()
	defer udp.Close()
	var wg sync.WaitGroup
	defer wg.Wait()
	go func() {
		<-ctx.Done()
		tcp.Close()
		udp.Close()
		s.mu.Lock()
		for c := range s.clients {
			c.conn.Close()
		}
		s.mu.Unlock()
	}()
	wg.Add(1)
	go func() { defer wg.Done(); s.serveUDP(ctx, udp) }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				s.mu.Lock()
				s.tickQuestTime(now)
				s.mu.Unlock()
			}
		}
	}()
	for {
		conn, e := tcp.Accept()
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			cancel()
			return e
		}
		c := &client{conn: conn, codec: protocol.NewCodec(), queue: make(chan outbound, 512)}
		s.mu.Lock()
		if ctx.Err() != nil {
			s.mu.Unlock()
			conn.Close()
			return nil
		}
		if len(s.clients) >= s.Config.MaxPlayers*2 {
			s.mu.Unlock()
			conn.Close()
			continue
		}
		s.clients[c] = true
		s.mu.Unlock()
		wg.Add(1)
		go func() { defer wg.Done(); s.serveClient(ctx, c) }()
	}
}
func (s *Server) serveClient(ctx context.Context, c *client) {
	var done chan struct{}
	defer func() {
		c.conn.Close()
		s.mu.Lock()
		delete(s.clients, c)
		s.removeDedicated(c)
		s.leaveRoom(c)
		if c.user.ID != 0 {
			delete(s.users, c.user.ID)
			s.lobbyLeft(c)
		}
		close(c.queue)
		s.mu.Unlock()
		if done != nil {
			<-done
		}
	}()
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, e := io.WriteString(c.conn, protocol.Greeting); e != nil {
		return
	}
	done = make(chan struct{})
	go func() {
		defer close(done)
		defer c.conn.Close()
		for msg := range c.queue {
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if e := c.codec.Write(c.conn, msg.id, msg.data); e != nil {
				return
			}
			if msg.crypt != nil {
				c.codec.Out = msg.crypt
			}
		}
	}()
	for {
		if c.user.ID != 0 || c.dedicated != nil {
			c.conn.SetReadDeadline(time.Time{})
		} else {
			c.conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		}
		p, e := c.codec.Read(c.conn)
		if e != nil {
			if !errors.Is(e, io.EOF) && ctx.Err() == nil {
				s.Log.Debug("connection closed", "peer", c.conn.RemoteAddr(), "error", e)
			}
			return
		}
		s.mu.Lock()
		s.Log.Debug("packet received", "id", p.ID, "size", len(p.Data), "user", c.user.ID)
		e = s.dispatch(c, p)
		s.mu.Unlock()
		if e != nil {
			s.Log.Debug("invalid request", "peer", c.conn.RemoteAddr(), "id", p.ID, "error", e)
			return
		}
	}
}
func (s *Server) dispatch(c *client, p protocol.Packet) error {
	r := &protocol.Reader{Data: p.Data}
	if c.dedicated != nil {
		switch p.ID {
		case 68:
			return s.hostRequest(c, r)
		case 81:
			return s.dedicatedRequest(c, r)
		case 12:
			if c.cryptIn != nil && c.codec.In == nil {
				c.codec.In = c.cryptIn
			}
			return nil
		default:
			return nil
		}
	}
	switch p.ID {
	case 81:
		return s.dedicatedRequest(c, r)
	case 0:
		if c.user.ID != 0 {
			return errors.New("version after authentication")
		}
		launcher := r.U8()
		r.U16()
		build := r.U32()
		r.U32()
		if r.Err != nil {
			return r.Err
		}
		if s.Config.CheckClientBuild && (launcher != s.Config.AllowedLauncherVersion || build != s.Config.AllowedClientTimestamp) {
			return errors.New("unsupported client version")
		}
		if c.version {
			c.chatNotice("Enter /login <username> <password> or /register <username> <password>.")
		} else {
			c.send(0, &protocol.Writer{Data: []byte{0}})
		}
		c.version = true
	case 3:
		if !c.version || c.loginHandshake || c.user.ID != 0 {
			return errors.New("unexpected login handshake")
		}
		r.CString()
		r.Bytes(r.U16())
		hwid := append([]byte{}, r.Bytes(16)...)
		r.U32()
		r.U32()
		r.CString()
		if r.Err != nil {
			return r.Err
		}
		c.hwid = hwid
		c.loginHandshake = true
		if s.Config.Crypt {
			return s.beginCrypt(c)
		}
	case 12:
		if !s.Config.Crypt {
			return nil
		}
		if !c.loginHandshake || c.cryptIn == nil || c.codec.In != nil {
			return errors.New("unexpected crypt acknowledgment")
		}
		c.codec.In = c.cryptIn
	case 67:
		if !c.version {
			return errors.New("version handshake required")
		}
		return s.message(c, r)
	case 10:
		s.leaveRoom(c)
		s.lobbyLeft(c)
		s.serverList(c)
	case 7:
		if c.user.ID == 0 {
			return errors.New("authentication required")
		}
		server, ch := r.U8(), r.U8()
		if r.Err != nil {
			return r.Err
		}
		if server == 1 && ch == 1 {
			s.joinLobby(c)
		}
	case 2:
		if c.user.ID == 0 {
			return errors.New("authentication required")
		}
		name := r.CString()
		if r.Err != nil {
			return r.Err
		}
		if c.character.Name != "" {
			return errors.New("character already exists")
		}
		if e := s.Store.CreateCharacter(c.user.ID, name, s.Config.DefaultUser); e != nil {
			c.notice(e.Error())
			return nil
		}
		var e error
		c.character, e = s.Store.Character(c.user.ID)
		if e != nil {
			return e
		}
		c.reply(1)
		return s.loginPackets(c)
	default:
		if c.user.ID == 0 || c.character.Name == "" {
			return errors.New("authentication and character required")
		}
		switch p.ID {
		case 65:
			return s.roomRequest(c, r)
		case 68:
			return s.hostRequest(c, r)
		case 69:
			return s.updateInfoRequest(c, r)
		case 76:
			if r.U8() == 0 {
				b := append([]byte{}, r.Bytes(r.U16())...)
				if r.Err != nil {
					return r.Err
				}
				if len(b) > protocol.MaxPayload-4 {
					return errors.New("player configuration exceeds response frame limit")
				}
				_, e := s.Store.DB.Exec("UPDATE UserCharacterExtended SET config=? WHERE userID=?", b, c.user.ID)
				return e
			}
		case 77:
			return s.favorite(c, r)
		case 72:
			return s.shopRequest(c, r)
		case 74:
			return s.banRequest(c, r)
		case 78:
			return s.itemRequest(c, r)
		case 84:
			return s.titleRequest(c, r)
		case 87:
			return s.surveyRequest(c, r)
		case 93:
			return s.messengerRequest(c, r)
		case 115:
			return s.addonRequest(c)
		case 112:
			return s.kickRequest(c, r)
		case 16:
			c.send(16, &protocol.Writer{Data: []byte{0, 1}})
		default:
			s.Log.Debug("unsupported packet", "id", p.ID, "user", c.user.ID)
		}
	}
	return r.Err
}
func (s *Server) beginCrypt(c *client) error {
	key, iv := protocol.DeriveKey(c.hwid)
	enc, _ := rc4.NewCipher(key[:16])
	dec, _ := rc4.NewCipher(key[:16])
	c.cryptIn = dec
	for i := 0; i < 2; i++ {
		w := &protocol.Writer{}
		w.U8(i)
		w.U8(2)
		w.Bytes(key[:])
		w.Bytes(iv[:])
		m := outbound{id: 7, data: w.Data}
		if i == 0 {
			m.crypt = enc
		}
		select {
		case c.queue <- m:
		default:
			return errors.New("output queue full")
		}
	}
	return nil
}
func (s *Server) serveUDP(ctx context.Context, conn *net.UDPConn) {
	buf := make([]byte, 1500)
	for {
		n, addr, e := conn.ReadFromUDP(buf)
		if e != nil {
			if ctx.Err() == nil {
				s.Log.Warn("UDP read failed", "error", e)
			}
			return
		}
		if n < 7 || buf[0] != 'W' {
			continue
		}
		r := &protocol.Reader{Data: buf[1:n]}
		uid, kind := r.U32(), r.U8()
		if kind != 0 {
			continue
		}
		portID := r.U8()
		local := append(net.IP{}, r.Bytes(4)...)
		port := r.U16()
		r.U8()
		if r.Err != nil || portID < 0 || portID > 1 {
			continue
		}
		for i := range local {
			local[i] = ^local[i]
		}
		s.mu.Lock()
		c := s.users[uid]
		ok := c != nil && c.ip() == addr.IP.String()
		if ok {
			c.localIP = local
			c.localPorts[portID] = port
			c.externalPorts[portID] = addr.Port
		}
		s.mu.Unlock()
		if ok {
			conn.WriteToUDP([]byte{'W', 0, 1}, addr)
		}
	}
}
