package server

import (
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

const serverYellItemID = 204

// The reference writes a fixed literal in the string slot of both lobby
// entries: SendLobbyJoin (src/manager/packetmanager.cpp:1756) uses "test_lobby"
// and SendLobbyUserJoin (src/manager/packetmanager.cpp:1770) uses
// "TIBE YEBAT'?". The client reads the name from the user info that follows, so
// both values are placeholders; they are reproduced verbatim so a byte
// comparison against the reference stays clean.
const lobbyJoinLabel = "test_lobby"
const lobbyUserJoinLabel = "TIBE YEBAT'?"

// achievementBitmap packs unlocked title IDs into the fixed 128-byte bitmap
// the client expects for the achievement list section.
func achievementBitmap(list []int) []byte {
	bitmap := make([]byte, 128)
	for _, id := range list {
		if id <= 0 || id >= len(bitmap)*8 {
			continue
		}
		bitmap[id/8] |= 1 << (id % 8)
	}
	return bitmap
}

// userInfo writes one character the way the reference does. Every caller passes
// UFLAG_LOW_ALL|UFLAG_HIGH_ALL there (GetCharacter(UFLAG_LOW_ALL,
// UFLAG_HIGH_ALL)), so CPacketHelper_FullUserInfo::Build
// (src/packet/packethelper_fulluserinfo.cpp:43) emits *every* section, filling
// the ones the reference has no data for with zeros. The flags are therefore
// always all-ones here too: a client that parsed our shorter variant would
// keep its defaults, which is not what it sees against the reference.
func userInfo(w *protocol.Writer, c store.Character) {
	const allFlags = 0xFFFFFFFF
	w.U32(allFlags)
	w.U32(allFlags)
	// UFLAG_LOW_NAMEPLATE: the byte in front of the id is a fixed type marker.
	w.U8(0)
	w.U32(c.NameplateID)
	// UFLAG_LOW_GAMENAME and UFLAG_LOW_GAMENAME2 both carry the game name; the
	// second one is followed by three zero bytes.
	w.String(c.Name)
	w.String(c.Name)
	w.U8(0)
	w.U8(0)
	w.U8(0)
	w.U32(c.Level)
	w.U8(0) // UFLAG_LOW_UNK4
	w.U64(int64(c.Exp))
	w.U64(int64(c.Cash))
	w.U64(int64(c.Points))
	// UFLAG_LOW_STAT: four live counters and sixteen fields the reference
	// hardcodes to zero.
	w.U32(c.Battles)
	w.U32(c.Win)
	w.U32(c.Kills)
	w.U32(c.Deaths)
	for i := 0; i < 16; i++ {
		w.U32(0)
	}
	// UFLAG_LOW_LOCATION: region name, the three place ids and the PC cafe name.
	w.String("")
	w.U16(c.Nation)
	w.U16(c.City)
	w.U16(c.Town)
	w.String("")
	w.U32(0) // UFLAG_LOW_CASH2
	w.U8(0)  // UFLAG_LOW_UNK11
	// UFLAG_LOW_CLAN
	w.U32(c.ClanID)
	w.U32(c.ClanMarkID)
	w.String(c.ClanName)
	w.U8(0)
	w.U8(0)
	w.U8(0)
	w.U8(c.Tournament) // UFLAG_LOW_TOURNAMENT
	// UFLAG_LOW_RANK: a 0xFF present marker, one tier byte per league and the
	// current league id. Rank tracking is not implemented, so the tiers stay at
	// the value a character without rank rows has in the reference.
	w.U8(0xFF)
	for i := 0; i < 4; i++ {
		w.U8(0)
	}
	w.U8(0)
	w.U8(0) // UFLAG_LOW_UNK15
	w.U8(0)
	w.U8(0)
	w.U16(c.PasswordBoxes) // UFLAG_LOW_PASSWORDBOXES
	w.U32(0)               // UFLAG_LOW_UNK17
	// UFLAG_LOW_ACHIEVEMENT: prefix id, two zero bytes, honor points.
	w.U16(c.PrefixID)
	w.U8(0)
	w.U8(0)
	w.U32(c.HonorPoints)
	w.Bytes(achievementBitmap(c.Achievements)) // UFLAG_LOW_ACHIEVEMENTLIST
	for i := 0; i < 5; i++ {                   // UFLAG_LOW_UNK20
		w.U16(0)
	}
	w.U8(0) // UFLAG_LOW_UNK21
	w.U32(0)
	for _, title := range c.Titles { // UFLAG_LOW_TITLES, always five slots
		w.U16(title)
	}
	// UFLAG_LOW_UNK23 is written twice by the reference: one byte here and two
	// more after UFLAG_LOW_UNK30.
	w.U8(0)
	w.U16(0)               // UFLAG_LOW_UNK25
	w.U32(c.MileagePoints) // UFLAG_LOW_UNK26
	w.U32(0)
	w.U32(c.ID)              // UFLAG_LOW_UNK27
	w.U8(0)                  // UFLAG_LOW_UNK28
	w.U8(0)                  // UFLAG_LOW_UNK29
	for i := 0; i < 7; i++ { // UFLAG_LOW_UNK30
		w.U32(0)
	}
	w.U8(0)
	w.U8(0)
	w.U8(0) // the second UFLAG_LOW_UNK23 block
	w.U8(0)
	w.U16(c.ChatColorID) // UFLAG_HIGH_CHATCOLOR
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
	// The reference sends the full room list twice on a first channel join:
	// CChannel::UserJoin (src/channel/channel.cpp:68) and
	// CChannelManager::JoinChannel (src/manager/channelmanager.cpp:207) both
	// call SendRoomListFull. A client that is already in the channel only sees
	// the latter.
	joined := !c.inLobby
	if joined {
		c.inLobby = true
		// CChannel::UserJoin pushes the user onto the channel's user vector, and
		// SendLobbyJoin walks that vector in order (src/channel/channel.cpp:57,
		// src/manager/packetmanager.cpp:1753). The sequence number keeps our map of
		// clients in the same order.
		s.lobbySeq++
		c.lobbySeq = s.lobbySeq
		w := &protocol.Writer{}
		w.U8(1)
		w.U32(c.user.ID)
		w.String(lobbyUserJoinLabel)
		userInfo(w, c.character)
		for u := range s.clients {
			if u != c && u.inLobby {
				u.send(153, w)
			}
		}
	}
	users := make([]*client, 0, len(s.clients))
	for u := range s.clients {
		if u.inLobby {
			users = append(users, u)
		}
	}
	sort.Slice(users, func(i, j int) bool {
		if users[i].lobbySeq != users[j].lobbySeq {
			return users[i].lobbySeq < users[j].lobbySeq
		}
		return users[i].user.ID < users[j].user.ID
	})
	w := &protocol.Writer{}
	w.U8(0)
	w.U16(len(users))
	for _, u := range users {
		w.U32(u.user.ID)
		w.String(lobbyJoinLabel)
		userInfo(w, u.character)
	}
	c.send(153, w)
	s.roomList(c)
	if joined {
		s.roomList(c)
	}
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
	if kind == 6 {
		// Server yell: consume one megaphone item and broadcast to the channel.
		items, err := s.Store.Inventory(c.user.ID)
		if err != nil {
			return err
		}
		owned := false
		for _, item := range items {
			if item.ID == serverYellItemID && item.Count > 0 {
				owned = true
				break
			}
		}
		if !owned {
			return nil
		}
		protected := append([]int{}, s.Config.DefaultUser.DefaultItems...)
		protected = append(protected, s.Config.DefaultUser.PseudoDefaultItems...)
		changed, err := s.Store.ConsumeItem(c.user.ID, serverYellItemID, protected)
		if errors.Is(err, store.ErrItemUnavailable) || errors.Is(err, store.ErrItemProtected) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = s.sendInventoryItems(c, changed); err != nil {
			return err
		}
		w := &protocol.Writer{}
		w.U8(6)
		w.String(c.character.Name)
		w.String(text)
		for u := range s.clients {
			if u.user.ID != 0 && u.character.Name != "" && s.chatAllowed(c, u) {
				u.send(67, w)
			}
		}
		return nil
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
			if old := s.users[u.ID]; old != nil && old != c {
				// A reconnect must take over the stale session instead of
				// waiting for the old socket to time out.
				s.endSession(u.ID)
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
			if sessionErr := s.Store.StartSession(u.ID, c.ip(), c.hwid, int(time.Now().Unix()/60)); sessionErr != nil {
				return sessionErr
			}
			c.reply(0)
			if errors.Is(e, sql.ErrNoRows) {
				c.send(6, &protocol.Writer{})
				return nil
			}
			return s.loginPackets(c)
		}
		if c.user.ID == 0 || c.character.Name == "" {
			c.chatNotice("Unknown command.")
			return nil
		}
		if c.character.GameMaster {
			handled, err := s.gmCommand(c, text, args)
			if handled {
				return err
			}
		}
		// Upstream exposes these two activity refresh commands to every user.
		if args[0] == "/weaponrelease" {
			if s.miniGame.Active {
				return s.sendWeaponReleaseUpdate(c)
			}
			return nil
		}
		if args[0] == "/bingo" {
			if s.bingo.Active {
				return s.sendBingoUpdate(c)
			}
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
			c.systemReply("MSG_TELL_USER_NOT_FOUND", target)
			return nil
		}
		if to == c {
			c.chatNotice("You can't send whisper to yourself.")
			return nil
		}
		if reason, denied := s.whisperDenied(c, to); denied {
			if reason != "" {
				c.systemReply(reason)
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
	// CUser is constructed before the login chain starts, so the network config
	// the room packets carry is the TCP peer address and the default ports until a
	// holepunch overwrites it (src/user/user.cpp:22-27).
	c.initNetwork()
	// The sweep still runs so this account starts from current data, but the
	// login chain initializes the inventory itself further down; pushing a bag
	// update here would put 154 in front of UserStart 150.
	s.sweepExpiredItems(time.Now(), c.user.ID)
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
	// CUserManager::SendLoginPacket (src/manager/usermanager.cpp:583) sends the
	// saved option block before the ban list, so the reference emits the 76
	// option frame ahead of the 74 ban frame.
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
	if e := s.sendBanLogin(c); e != nil {
		return e
	}
	for _, b := range s.metadata {
		if len(b) != 0 {
			s.Log.Debug("metadata", "type", b[0], "len", len(b))
		}
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
	w.U32(s.eventFlags())
	c.send(159, w)
	s.sendMainMenuSkin(c)
	if s.miniGame.Active {
		if e := s.sendWeaponReleaseUpdate(c); e != nil {
			return e
		}
	}
	if s.bingo.Active {
		if e := s.sendBingoUpdate(c); e != nil {
			return e
		}
	}
	if e := s.sendInventory(c); e != nil {
		return e
	}
	if e := s.sendFavorites(c); e != nil {
		return e
	}
	// Remaining order mirrors CUserManager::SendLoginPacket: notices
	// (usermanager.cpp:611), shop (:613), the hard-coded welcome (:619), the
	// configured welcome (:622), the channel join (:624), surveys (:626),
	// league (:632), the update-info marker (:635) and the voxel urls (:637).
	s.sendNotices(c)
	s.sendShop(c)
	c.notice(loginWelcomeText)
	if s.Config.WelcomeMessage != "" {
		c.notice(s.Config.WelcomeMessage)
	}
	s.joinLobby(c)
	if e := s.sendClanLogin(c); e != nil {
		return e
	}
	if e := s.sendSurveys(c); e != nil {
		return e
	}
	s.sendLeague(c)
	c.send(69, &protocol.Writer{Data: []byte{11}})
	s.sendVoxelURLs(c)
	if e := s.grantRewardNotices(c); e != nil {
		return e
	}
	if e := s.sendExpiryNotices(c); e != nil {
		return e
	}
	if !c.questLogin {
		if err := s.applyQuestEvent(c, nil, nil, questEvent{Kind: 3}); err != nil {
			return err
		}
		c.questLogin = true
	}
	if e := s.resumeQuestRewards(c); e != nil {
		return e
	}
	s.restoreSession(c)
	return nil
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
	case 1:
		slot := r.U8()
		name := r.CString()
		items := make([]int, 11)
		for i := range items {
			items[i] = r.U16()
		}
		if r.Err != nil {
			return r.Err
		}
		if r.Pos != len(r.Data) {
			return errors.New("unexpected fastbuy request tail")
		}
		return s.Store.SetFastBuy(c.user.ID, slot, name, items)
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
func (s *Server) sendLeague(c *client) {
	w := &protocol.Writer{}
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
}
