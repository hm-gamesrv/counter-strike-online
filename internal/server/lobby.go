package server

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func userInfo(w *protocol.Writer, c store.Character) {
	// Only advertise fields actually serialized below; absent flags preserve
	// client defaults and avoid fabricating clan, ranking or achievement state.
	w.U32(1<<1 | 1<<3 | 1<<5 | 1<<6 | 1<<7 | 1<<8 | 1<<9 | 1<<16 | 1<<18 | 1<<26 | 1<<27)
	w.U32(1)
	w.String(c.Name)
	w.U32(c.Level)
	w.U64(int64(c.Exp))
	w.U64(int64(c.Cash))
	w.U64(int64(c.Points))
	w.U32(c.Battles)
	w.U32(c.Win)
	w.U32(c.Kills)
	w.U32(c.Deaths)
	for i := 0; i < 16; i++ {
		w.U32(0)
	}
	w.String("")
	w.U16(c.Nation)
	w.U16(c.City)
	w.U16(c.Town)
	w.String("")
	w.U16(c.PasswordBoxes)
	w.U16(c.PrefixID)
	w.U8(0)
	w.U8(0)
	w.U32(c.HonorPoints)
	w.U32(c.MileagePoints)
	w.U32(0)
	w.U32(c.ID)
	w.U16(c.ChatColorID)
}
func (s *Server) serverList(c *client) {
	w := &protocol.Writer{}
	w.U8(1)
	w.U8(1)
	w.U8(1)
	w.U8(0)
	w.String(s.Config.HostName)
	w.U8(1)
	w.U8(1)
	w.String("Channel 1")
	w.U16(0)
	c.send(5, w)
}
func (s *Server) joinLobby(c *client) {
	if c.user.ID == 0 || c.character.Name == "" {
		return
	}
	s.leaveRoom(c)
	if !c.inLobby {
		c.inLobby = true
		w := &protocol.Writer{}
		w.U8(1)
		w.U32(c.user.ID)
		w.String(c.user.Name)
		userInfo(w, c.character)
		for u := range s.clients {
			if u != c && u.inLobby {
				u.send(153, w)
			}
		}
	}
	w := &protocol.Writer{}
	w.U8(0)
	n := 0
	for u := range s.clients {
		if u.inLobby {
			n++
		}
	}
	w.U16(n)
	for u := range s.clients {
		if u.inLobby {
			w.U32(u.user.ID)
			w.String(u.user.Name)
			userInfo(w, u.character)
		}
	}
	c.send(153, w)
	s.roomList(c)
}
func (s *Server) lobbyLeft(c *client) {
	if !c.inLobby {
		return
	}
	c.inLobby = false
	w := &protocol.Writer{}
	w.U8(2)
	w.U32(c.user.ID)
	for u := range s.clients {
		if u.inLobby {
			u.send(153, w)
		}
	}
}
func (s *Server) message(c *client, r *protocol.Reader) error {
	kind := r.U8()
	if kind == 67 {
		return s.rewardSelection(c, r)
	}
	target := ""
	if kind == 0 {
		target = r.CString()
	}
	text := r.CString()
	if r.Err != nil {
		return r.Err
	}
	if len(text) > 1024 {
		return errors.New("chat message too long")
	}
	if kind == 1 && strings.HasPrefix(text, "/") {
		args := strings.Fields(text)
		if len(args) == 0 {
			return nil
		}
		switch args[0] {
		case "/register":
			if c.user.ID != 0 {
				c.notice("Already logged in.")
				return nil
			}
			if len(args) != 3 {
				c.chatNotice("/register <username> <password>")
				return nil
			}
			if e := s.Store.Register(args[1], args[2], c.ip(), s.Config.MaxRegistrationsPerIP); e != nil {
				c.notice(e.Error())
			} else {
				c.notice("You have successfully registered.")
			}
			return nil
		case "/login":
			if c.user.ID != 0 {
				c.reply(4)
				return nil
			}
			if len(args) != 3 {
				c.chatNotice("/login <username> <password>")
				return nil
			}
			if s.Config.Crypt && c.codec.In == nil {
				return errors.New("crypt handshake required")
			}
			u, e := s.Store.Login(args[1], args[2], c.ip(), c.hwid)
			if e != nil {
				c.notice(e.Error())
				return nil
			}
			if s.users[u.ID] != nil {
				c.reply(4)
				return nil
			}
			if len(s.users) >= s.Config.MaxPlayers {
				c.reply(26)
				return nil
			}
			ch, e := s.Store.Character(u.ID)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return e
			}
			c.user = u
			c.character = ch
			s.users[u.ID] = c
			c.reply(0)
			if errors.Is(e, sql.ErrNoRows) {
				c.send(6, &protocol.Writer{})
				return nil
			}
			return s.loginPackets(c)
		case "/version":
			c.chatNotice("CSNZ Go server; client build 30.12.24")
			return nil
		default:
			c.chatNotice("Unknown command.")
			return nil
		}
	}
	if c.user.ID == 0 || c.character.Name == "" {
		c.chatNotice("Enter /login <username> <password> or /register <username> <password>.")
		return nil
	}
	if kind == 0 {
		var to *client
		for u := range s.clients {
			if strings.EqualFold(u.character.Name, target) {
				to = u
				break
			}
		}
		if to == nil {
			c.notice("MSG_TELL_USER_NOT_FOUND")
			return nil
		}
		if reason, denied := s.whisperDenied(c, to); denied {
			if reason != "" {
				c.notice(reason)
			}
			return nil
		}
		w := &protocol.Writer{}
		w.U8(0)
		w.U8(0)
		w.String(target)
		w.String(text)
		c.send(67, w)
		w = &protocol.Writer{}
		w.U8(0)
		w.U8(1)
		w.String(c.character.Name)
		w.String(text)
		to.send(67, w)
		return nil
	}
	if kind != 1 && kind != 2 && kind != 4 {
		return nil
	}
	w := &protocol.Writer{}
	w.U8(kind)
	w.String(c.character.Name)
	w.String(text)
	for u := range s.clients {
		if (kind == 1 && c.inLobby && u.inLobby || (kind == 2 || kind == 4) && c.room != 0 && u.room == c.room && (kind != 4 || u.team == c.team)) && s.chatAllowed(c, u) {
			u.send(67, w)
		}
	}
	return nil
}
func (s *Server) loginPackets(c *client) error {
	w := &protocol.Writer{}
	w.U32(c.user.ID)
	w.String(c.user.Name)
	w.String(c.character.Name)
	w.U8(1)
	w.U8(0)
	w.U8(0)
	w.U32(0)
	w.U8(0)
	c.send(150, w)
	w = &protocol.Writer{}
	w.U32(c.user.ID)
	userInfo(w, c.character)
	c.send(157, w)
	if e := s.sendBanLogin(c); e != nil {
		return e
	}
	var cfg []byte
	if e := s.Store.DB.QueryRow("SELECT config FROM UserCharacterExtended WHERE userID=?", c.user.ID).Scan(&cfg); e != nil {
		return e
	}
	if len(cfg) != 0 {
		if len(cfg) > protocol.MaxPayload-4 {
			return errors.New("saved player configuration exceeds frame limit")
		}
		w = &protocol.Writer{}
		w.U8(0)
		w.U16(len(cfg))
		w.Bytes(cfg)
		w.U8(1)
		c.send(76, w)
	}
	for _, b := range s.metadata {
		c.send(91, &protocol.Writer{Data: b})
	}
	// Match tabs and state markers are required by the 2024 client UI.
	w = &protocol.Writer{}
	w.U8(40)
	w.U16(45)
	w.U8(2)
	w.U8(1)
	w.String("")
	w.U8(4)
	for i, name := range []string{"Zombie3z", "quickstart", "roomlist", "studio"} {
		w.U8(i)
		k := []int{3, 0, 1, 3}[i]
		w.U8(k)
		w.U16([]int{45, 0, 0, 38}[i])
		w.String(name)
	}
	c.send(99, w)
	c.send(99, &protocol.Writer{Data: []byte{0x65, 1, 0, 14, 0, 2, 0, 255}})
	w = &protocol.Writer{}
	w.U8(9)
	w.U16(32)
	w.U16(14)
	w.U16(2)
	w.U16(65535)
	c.send(99, w)
	c.send(159, &protocol.Writer{Data: []byte{0, 0}})
	w = &protocol.Writer{}
	w.U8(1)
	w.U32(0)
	c.send(159, w)
	if e := s.sendInventory(c); e != nil {
		return e
	}
	if e := s.sendFavorites(c); e != nil {
		return e
	}
	s.sendShop(c)
	w = &protocol.Writer{}
	w.U8(0)
	w.U8(5)
	for i := 0; i < 5; i++ {
		w.U8(i)
		w.U8(1)
		w.U16(1)
		w.U8(5)
		w.U32(0)
	}
	c.send(180, w)
	s.joinLobby(c)
	c.send(69, &protocol.Writer{Data: []byte{11}})
	if s.Config.WelcomeMessage != "" {
		c.notice(s.Config.WelcomeMessage)
	}
	if !c.questLogin {
		if err := s.applyQuestEvent(c, nil, nil, questEvent{Kind: 3}); err != nil {
			return err
		}
		c.questLogin = true
	}
	return s.resumeQuestRewards(c)
}
func writeItem(w *protocol.Writer, i store.Item, slot, disassemble int) {
	w.U16(slot)
	if i.ID == 0 {
		w.U8(0)
		return
	}
	w.U8(1)
	w.U16(i.ID)
	w.U16(i.Count)
	w.U8(i.Status)
	w.U8(i.InUse)
	w.U32(i.Obtain)
	w.U32(i.Expiry)
	w.U16(i.Paint)
	w.U16(len(i.Paints))
	for _, v := range i.Paints {
		w.U16(v)
	}
	w.U16(i.Level)
	w.U32(i.Exp)
	w.U32(i.Value)
	w.U8(disassemble)
	w.U32(0)
	parts := 0
	if i.Part1 != 0 {
		parts++
	}
	if i.Part2 != 0 {
		parts++
	}
	w.U8(parts)
	if i.Part1 != 0 {
		w.U8(0)
		w.U16(i.Part1)
	}
	if i.Part2 != 0 {
		w.U8(1)
		w.U16(i.Part2)
	}
	w.U8(i.InUse)
	w.U8(0)
	w.U8(i.Lock)
	w.U32(0)
	w.U8(0)
}
func (s *Server) sendInventory(c *client) error {
	w, e := defaultInventoryPacket(s.Config.DefaultUser.DefaultItems)
	if e != nil {
		return e
	}
	items, e := s.Store.Inventory(c.user.ID)
	if e != nil {
		return e
	}
	packets, e := inventoryPackets(items, len(s.Config.DefaultUser.DefaultItems))
	if e != nil {
		return e
	}
	c.send(152, w)
	for _, data := range packets {
		c.send(154, &protocol.Writer{Data: data})
	}
	return nil
}
func (s *Server) sendFavorites(c *client) error {
	w := &protocol.Writer{}
	w.U8(2)
	var character, current int
	if e := s.Store.DB.QueryRow("SELECT characterID,curLoadout FROM UserCharacterExtended WHERE userID=?", c.user.ID).Scan(&character, &current); e != nil {
		return e
	}
	w.U16(character)
	w.U8(current)
	w.U8(12)
	w.U8(4)
	rows, e := s.Store.DB.Query("SELECT slot0,slot1,slot2,slot3 FROM UserLoadout WHERE userID=? ORDER BY loadoutID", c.user.ID)
	if e != nil {
		return e
	}
	for rows.Next() {
		var a [4]int
		if e = rows.Scan(&a[0], &a[1], &a[2], &a[3]); e != nil {
			rows.Close()
			return e
		}
		for _, v := range a {
			w.U16(v)
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	c.send(77, w)
	w = &protocol.Writer{}
	w.U8(0)
	rows, e = s.Store.DB.Query("SELECT buyMenuID,slot1,slot2,slot3,slot4,slot5,slot6,slot7,slot8,slot9 FROM UserBuyMenu WHERE userID=? ORDER BY buyMenuID", c.user.ID)
	if e != nil {
		return e
	}
	for rows.Next() {
		var a [10]int
		ptr := make([]any, 10)
		for i := range a {
			ptr[i] = &a[i]
		}
		if e = rows.Scan(ptr...); e != nil {
			rows.Close()
			return e
		}
		w.U8(a[0])
		for _, v := range a[1:] {
			w.U16(v)
		}
		w.U16(0)
		w.U16(0)
		w.U16(0)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	c.send(77, w)
	w = &protocol.Writer{}
	w.U8(6)
	w.U8(8)
	for i := 0; i < 8; i++ {
		var v int
		if e = s.Store.DB.QueryRow("SELECT itemID FROM UserBookmark WHERE userID=? AND bookmarkID=?", c.user.ID, i).Scan(&v); e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		w.U16(v)
	}
	c.send(77, w)
	return nil
}
func (s *Server) favorite(c *client, r *protocol.Reader) error {
	kind := r.U8()
	var query string
	var args []any
	switch kind {
	case 0:
		menu, slot, item := r.U8(), r.U8(), r.U16()
		if menu >= 17 || slot >= 9 {
			return errors.New("invalid buy menu slot")
		}
		query = "UPDATE UserBuyMenu SET slot" + strconv.Itoa(slot+1) + "=? WHERE userID=? AND buyMenuID=?"
		args = []any{item, c.user.ID, menu}
	case 6:
		slot, item := r.U8(), r.U16()
		if slot >= 8 {
			return errors.New("invalid bookmark")
		}
		query = "UPDATE UserBookmark SET itemID=? WHERE userID=? AND bookmarkID=?"
		args = []any{item, c.user.ID, slot}
	case 2:
		kind := r.U8()
		if kind == 1 {
			slot := r.U8()
			if slot >= 12 {
				return errors.New("invalid loadout")
			}
			query = "UPDATE UserCharacterExtended SET curLoadout=? WHERE userID=?"
			args = []any{slot, c.user.ID}
		} else {
			return s.setLoadoutItem(c, kind, r)
		}
	default:
		return nil
	}
	if r.Err != nil {
		return r.Err
	}
	if query == "" {
		return nil
	}
	_, e := s.Store.DB.Exec(query, args...)
	return e
}

func (s *Server) setLoadoutItem(c *client, kind int, r *protocol.Reader) error {
	item := r.U16()
	if r.Err != nil {
		return r.Err
	}
	definition, exists := s.items[item]
	if !exists {
		c.notice("Item unavailable.")
		return nil
	}
	owned := false
	for _, id := range s.Config.DefaultUser.DefaultItems {
		if id == item {
			owned = true
			break
		}
	}
	if !owned {
		items, err := s.Store.Inventory(c.user.ID)
		if err != nil {
			return err
		}
		for _, i := range items {
			if i.ID == item && i.Count > 0 {
				owned = true
				break
			}
		}
	}
	if !owned {
		c.notice("Item unavailable.")
		return nil
	}
	if kind == 0 && definition.Category == 7 {
		_, err := s.Store.DB.Exec("UPDATE UserCharacterExtended SET characterID=? WHERE userID=?", item, c.user.ID)
		return err
	}
	var current int
	if err := s.Store.DB.QueryRow("SELECT curLoadout FROM UserCharacterExtended WHERE userID=?", c.user.ID).Scan(&current); err != nil {
		return err
	}
	slot := kind - (current+1)*10
	weapon := definition.Category >= 1 && definition.Category <= 6 || definition.Category == 11
	if current < 0 || current >= 12 || slot < 0 || slot >= 4 || !weapon {
		c.notice("Invalid loadout item or slot.")
		return nil
	}
	_, err := s.Store.DB.Exec("UPDATE UserLoadout SET slot"+strconv.Itoa(slot)+"=? WHERE userID=? AND loadoutID=?", item, c.user.ID, current)
	return err
}
