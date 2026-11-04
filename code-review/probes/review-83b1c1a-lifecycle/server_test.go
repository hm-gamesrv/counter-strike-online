package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func review83Log() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// drainNotices returns the text of every 67-subtype-10 notice queued on c.
func drainNotices(c *client) []string {
	var texts []string
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id != 67 {
			continue
		}
		r := &protocol.Reader{Data: msg.data}
		if r.U8() == 10 {
			texts = append(texts, r.CString())
		}
	}
	return texts
}

// The reference keeps familyBattleClanID1 at zero until a settings update or a
// host migration assigns it (roomsettings.cpp:496, room.cpp:993-1000); the first
// clan-bearing joiner therefore fills clan2 even when it is the host's own clan
// (channelmanager.cpp:1340-1352). Start then only needs clan2 != 0 and one ready
// non-host player (channelmanager.cpp:1616-1622). Go instead seeds clan1 from the
// creator (rooms.go:538-540) and startGame replaces both checks with "one member
// of each clan" (host.go:55, room_clan.go:206-224).
func TestReview83FamilyBattleStartGate(t *testing.T) {
	t.Run("same-clan-only-room", func(t *testing.T) {
		s, a, _, _, d := familyBattleSetup(t)
		s.Log = review83Log()
		s.clients = map[*client]bool{a: true, d: true}
		st := familySettings()
		st.setNumber(8, 8, 1)
		r := &room{id: 7, host: a, members: []*client{a, d}, settings: st}
		if !s.familyBattleInit(r, a) || r.clan1 == 0 {
			t.Fatal("host clan slot missing")
		}
		if !s.familyBattleJoin(r, d) {
			t.Fatal("same-clan member rejected")
		}
		if r.clan2 != 0 {
			t.Fatalf("same-clan member filled clan2: %+v", r)
		}
		a.room, d.room = 7, 7
		a.ready, d.ready = 0, 2
		s.rooms = map[int]*room{7: r}
		if err := s.startGame(a); err != nil {
			t.Fatal(err)
		}
		if r.match == nil {
			t.Fatalf("family battle start refused for a same-clan room (notices %q); the reference starts it because clan2 is filled by any first joiner", drainNotices(a))
		}
	})
	t.Run("no-ready-player", func(t *testing.T) {
		s, a, b, _, _ := familyBattleSetup(t)
		s.Log = review83Log()
		s.clients = map[*client]bool{a: true, b: true}
		st := familySettings()
		st.setNumber(8, 8, 1)
		r := &room{id: 7, host: a, members: []*client{a, b}, settings: st}
		if !s.familyBattleInit(r, a) || !s.familyBattleJoin(r, b) || r.clan2 == 0 {
			t.Fatal("opposing clan did not fill clan2")
		}
		a.room, b.room = 7, 7
		a.ready, b.ready = 0, 0
		s.rooms = map[int]*room{7: r}
		if err := s.startGame(a); err != nil {
			t.Fatal(err)
		}
		if r.match != nil {
			t.Fatalf("match started with zero ready players; the reference refuses with ROOM_START_FAILED_CLAN_NOT_READY (channelmanager.cpp:1618)")
		}
	})
}

// channelmanager.cpp:1765-1777 reassigns teams by comparing each member's clan
// with roomSettings->familyBattleClanID1, i.e. the settings value from before the
// update. When family battle is switched on that value is 0, so every other
// clan-bearing member is put on the opposite team. room_clan.go:150-180 compares
// against the freshly computed r.clan1 and keeps same-clan members with the host.
func TestReview83FamilyBattleRegroupSeparatesClanmates(t *testing.T) {
	s, a, b, _, d := familyBattleSetup(t)
	st := familySettings()
	st.setNumber(8, 8, 1)
	r := &room{id: 7, host: a, members: []*client{a, d, b}, settings: st}
	a.team, d.team, b.team = 2, 2, 2
	if _, ok := s.familyBattleSettings(r, &st); !ok {
		t.Fatal("family battle settings rejected")
	}
	s.regroupFamilyBattle(r)
	if d.team != 1 {
		t.Fatalf("host clanmate kept team %d (host team %d); the reference compares against the old clan slot 0 and moves every other member to the opposite team", d.team, a.team)
	}
	if b.team != 1 {
		t.Fatalf("opposing clan team %d", b.team)
	}
}

// CRoom::HostStartGame fills m_FamilyBattleUsers once (room.cpp:1082-1088) and
// nothing removes an entry until the next start; OnJoinRoomRequest only consults
// it while the room is INGAME (channelmanager.cpp:1354). Go derives the list from
// match.stats (room_clan.go:120-126) and leaveRoom deletes the stat, so a player
// who left and wants to rejoin the same match is refused.
func TestReview83FamilyBattleRejoinAfterLeave(t *testing.T) {
	s, a, b, _, _ := familyBattleSetup(t)
	st := familySettings()
	st.setNumber(8, 8, 1)
	r := &room{
		id:       7,
		host:     a,
		members:  []*client{a, b},
		settings: st,
		match: &gameMatch{
			id:   "review83-match",
			mode: 0,
			stats: map[int]*matchStat{
				1: {user: a},
				2: {user: b},
			},
		},
	}
	a.room, b.room = 7, 7
	a.playing, b.playing = true, true
	a.team, b.team = 2, 1
	s.rooms = map[int]*room{7: r}
	s.leaveRoom(b)
	if b.room != 0 || r.match == nil {
		t.Fatalf("leave state room=%d match=%v", b.room, r.match)
	}
	b.inLobby = true
	w := &protocol.Writer{}
	w.U8(23)
	w.U8(0)
	w.U16(7)
	w.String("")
	if err := s.roomRequest(b, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	if b.room != 7 {
		t.Fatalf("rejoin refused (notices %q); the reference still has user 2 in m_FamilyBattleUsers and accepts the join", drainNotices(b))
	}
}

// The reference computes rewards from the host-supplied counters and then pays
// them: exp is clamped to 30069814 by UpdateExp (user.cpp:295-317), points are
// added as-is by UpdatePoints (user.cpp:207-220). Go validates the already
// computed total against 10,000,000 (match.go:31) and aborts the whole
// settlement, so one extreme host counter blocks every participant forever.
func TestReview83SettlementRejectsExtremeHostCounter(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "settle.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("hostuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "HostUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	// Class item 55 is active, so the configured 20% class bonus applies.
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,1,55,1,1,1,?,0,0,0,0,0,0,0,0,0)", 1); err != nil {
		t.Fatal(err)
	}
	c := pipeClient(t, 1, "HostUser")
	c.queue = make(chan outbound, 64)
	st := settings{}
	st.setNumber(8, 8, 1)
	coef := struct {
		Exp    int
		Points int
	}{Exp: 320, Points: 290}
	s := &Server{
		Store: db,
		Log:   review83Log(),
		users: map[int]*client{1: c},
		clients: map[*client]bool{
			c: true,
		},
		rooms: map[int]*room{},
	}
	s.Config.GameMatch.CalcResults.GameModeCoefficient = map[string]struct {
		Exp    int
		Points int
	}{"20": coef}
	s.Config.GameMatch.CalcResults.BonusPercentage.Classes = map[string]struct {
		Exp    int
		Points int
	}{"55": {Exp: 20, Points: 20}}
	r := &room{
		id:       7,
		host:     c,
		members:  []*client{c},
		settings: st,
		match: &gameMatch{
			id:   "review83-extreme",
			mode: 20,
			stats: map[int]*matchStat{
				1: {user: c, kills: 1000000, class: 55},
			},
		},
	}
	c.room = 7
	c.playing = true
	s.rooms[7] = r
	err = s.endGame(r, false)
	if err != nil {
		t.Fatalf("settlement aborted: %v; the reference clamps the same counters (exp 30069814) and pays points", err)
	}
	ch, err := db.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Exp != 30069814 || ch.Points != 58009999 {
		t.Fatalf("paid exp=%d points=%d, want the reference's clamped values", ch.Exp, ch.Points)
	}
}

// Duration -1 in Shop.json/ItemRewards.json means permanent: the reference
// clamps every negative duration to 0 (itemmanager.cpp:683-684) and inserts the
// first copy with status=1,inUse=1. prepareGrants turns -1 into
// "Duration=0, Dormant=true" (rewards.go:87-90), which purchase.go:205-208 stores
// with status=0,inUse=0.
func TestReview83MinusOneDurationIsNotDormant(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	items, err := loadItems(dir)
	if err != nil {
		t.Fatal(err)
	}
	shopData, err := loadShop(dir)
	if err != nil {
		t.Fatal(err)
	}
	var product *subproduct
	for i := range shopData.Products {
		if shopData.Products[i].Item != 255 {
			continue
		}
		product = &shopData.Products[i].Subs[0]
	}
	if product == nil || len(product.Items) != 1 || product.Items[0].Duration != -1 {
		t.Fatalf("shipped Shop.json no longer has the -1 duration product 255: %+v", product)
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "shop.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("shopuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "ShopUser", config.DefaultUser{Level: 1, Points: 1000}); err != nil {
		t.Fatal(err)
	}
	c := pipeClient(t, 1, "ShopUser")
	c.queue = make(chan outbound, 64)
	s := &Server{
		Store: db,
		Log:   review83Log(),
		items: items,
		shop:  shopData,
		Config: config.Config{
			InventorySlotMax: 100,
		},
		clients: map[*client]bool{
			c: true,
		},
	}
	w := &protocol.Writer{}
	w.U8(1)
	w.U32(product.ID)
	if err = s.shopRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	var reply string
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id != 72 {
			continue
		}
		r := &protocol.Reader{Data: msg.data}
		if r.U8() == 1 {
			r.U8()
			reply = r.CString()
		}
	}
	if reply != "BUY_OK" {
		t.Fatalf("purchase reply %q", reply)
	}
	var status, inUse, expiry int
	if err = db.DB.QueryRow("SELECT status,inUse,expiryDate FROM UserInventory WHERE userID=1 AND itemID=255").Scan(&status, &inUse, &expiry); err != nil {
		t.Fatal(err)
	}
	if status != 1 || inUse != 1 || expiry != 0 {
		t.Fatalf("permanent -1 shop item stored status=%d inUse=%d expiry=%d; the reference stores status=1 inUse=1 expiry=0", status, inUse, expiry)
	}
}

// Each accepted connection runs one reader and one writer goroutine
// (server.go:303, :335) and serveClient closes the queue in its defer. This probe
// checks that closing N idle connections returns the process to its pre-connection
// goroutine count and empties the client map, so a per-connection goroutine does
// not outlive its socket.
func TestReview83ConnectionGoroutinesExit(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(dir, "ServerConfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Crypt = false
	db, err := store.Open(filepath.Join(dir, "UserDatabase.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv, err := New(dir, cfg, db, review83Log())
	if err != nil {
		t.Fatal(err)
	}
	tcp, udp := listenTestPair(t)
	addr, err := net.ResolveUDPAddr("udp4", udp.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = addr
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, tcp, udp) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("shutdown hung")
		}
	}()
	time.Sleep(200 * time.Millisecond)
	base := runtime.NumGoroutine()
	const connections = 16
	var peers []net.Conn
	for i := 0; i < connections; i++ {
		conn, err := net.Dial("tcp4", tcp.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		greeting := make([]byte, len(protocol.Greeting))
		if _, err = io.ReadFull(conn, greeting); err != nil || string(greeting) != protocol.Greeting {
			t.Fatalf("greeting %q %v", greeting, err)
		}
		conn.SetReadDeadline(time.Time{})
		peers = append(peers, conn)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		live := len(srv.clients)
		srv.mu.Unlock()
		if live == connections {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, conn := range peers {
		conn.Close()
	}
	for time.Now().Before(deadline) {
		srv.mu.Lock()
		live := len(srv.clients)
		srv.mu.Unlock()
		if live == 0 && runtime.NumGoroutine() <= base+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("connections not cleaned up: clients=%d goroutines base=%d now=%d", liveClients(srv), base, runtime.NumGoroutine())
}

func liveClients(s *Server) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}
