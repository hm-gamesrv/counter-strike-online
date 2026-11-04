package server

import (
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// The reference sends this exact msg box on every login
// (manager/usermanager.cpp:617-619, SendUMsgNoticeMsgBoxToUuid).
const baselineLoginWelcome = "EN: Welcome to the CSN:S server! The project is non-commercial. Don't trust people trying to sell you a server.\nServer developer Discord: https://discord.gg/EvUAY6D \n"

// stubConn gives a socket-less client the one method room packets need.
type stubConn struct {
	net.Conn
}

func (stubConn) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1234}
}

func (stubConn) Close() error {
	return nil
}

func drain(c *client) []outbound {
	var seen []outbound
	for {
		select {
		case msg := <-c.queue:
			seen = append(seen, msg)
		default:
			return seen
		}
	}
}

func ids(seen []outbound) []int {
	out := make([]int, 0, len(seen))
	for _, msg := range seen {
		out = append(out, int(msg.id))
	}
	return out
}

func indexOf(list []int, want int) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

func countOf(list []int, want int) int {
	total := 0
	for _, v := range list {
		if v == want {
			total++
		}
	}
	return total
}

func firstIndex(seen []outbound, want byte) int {
	for i, msg := range seen {
		if msg.id == want {
			return i
		}
	}
	return -1
}

// newLobbyProbeServer returns a login-capable server whose configuration is the
// one the live instance uses (welcome message, one notice, voxel URLs) and a
// client that already has an account and a character.
func newLobbyProbeServer(t *testing.T, voxel bool) (*Server, *client, *store.Store) {
	t.Helper()
	s, c, db := newLoginExtraServer(t)
	c.queue = make(chan outbound, 4096)
	// The fixture account has no account name; login fills it from the database.
	c.user.Name = "loginuser"
	s.Config.WelcomeMessage = "https://discord.gg/EvUAY6D"
	s.Config.Voxel.VoxelVxlURL = "http://example.invalid/%s.vxl"
	s.Config.Voxel.VoxelVmgURL = "https://example.invalid/%s.vmg"
	if !voxel {
		s.Config.Voxel.VoxelVxlURL = ""
		s.Config.Voxel.VoxelVmgURL = ""
	}
	s.notices = []noticeDefinition{{
		ID:          1,
		Type:        0,
		Name:        "Server information",
		Description: "Welcome to the CSO server.",
	}}
	return s, c, db
}

func loginSequence(t *testing.T, s *Server, c *client) []int {
	t.Helper()
	if err := s.loginPackets(c); err != nil {
		t.Fatal(err)
	}
	return ids(drain(c))
}

// 74 (BanSettingsReply) must follow 76 (Option): the reference sends the saved
// client config first (manager/usermanager.cpp:582-591).
func TestReview83LoginOptionBeforeBan(t *testing.T) {
	s, c, db := newLobbyProbeServer(t, true)
	if _, err := db.DB.Exec("UPDATE UserCharacterExtended SET config=? WHERE userID=1", []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO UserBanList(userID,gameName) VALUES(1,'OtherUser')"); err != nil {
		t.Fatal(err)
	}
	seq := loginSequence(t, s, c)
	t.Logf("login sequence: %v", seq)
	option, ban := indexOf(seq, 76), indexOf(seq, 74)
	if option < 0 || ban < 0 {
		t.Fatalf("missing 74/76 in %v", seq)
	}
	if option > ban {
		t.Fatalf("login sends 74 before 76; reference sends 76 then 74: %v", seq)
	}
}

// 103 (VoxelURLs) is the last packet of the reference login chain
// (manager/usermanager.cpp:637), after 69 (SendUpdateInfo, :635).
func TestReview83LoginVoxelURLsPosition(t *testing.T) {
	s, c, _ := newLobbyProbeServer(t, true)
	seq := loginSequence(t, s, c)
	voxel, update := indexOf(seq, 103), indexOf(seq, 69)
	if voxel < 0 {
		t.Fatalf("no 103 in %v", seq)
	}
	if voxel < update {
		t.Fatalf("103 sent at %d, before 69 at %d; reference sends 103 last: %v", voxel, update, seq)
	}
}

// The reference always calls SendVoxelURLs even when both configured URLs are
// empty, so the packet still exists (manager/usermanager.cpp:637).
func TestReview83LoginVoxelURLsSentWhenEmpty(t *testing.T) {
	s, c, _ := newLobbyProbeServer(t, false)
	seq := loginSequence(t, s, c)
	if indexOf(seq, 103) < 0 {
		t.Fatalf("no 103 with empty voxel URLs; reference still sends it: %v", seq)
	}
}

// JoinChannel sends the full room list twice: CChannel::UserJoin
// (channel/channel.cpp:68) and the end of CChannelManager::JoinChannel
// (manager/channelmanager.cpp:207).
func TestReview83LoginSendsRoomListTwice(t *testing.T) {
	s, c, _ := newLobbyProbeServer(t, true)
	seq := loginSequence(t, s, c)
	if got := countOf(seq, 151); got != 2 {
		t.Fatalf("login sent %d room lists, reference sends 2: %v", got, seq)
	}
}

// The login league packet (180 type 0) comes after the lobby join in the
// reference: JoinChannel at manager/usermanager.cpp:624, SendLeaguePacket :632.
func TestReview83LoginLeagueAfterLobby(t *testing.T) {
	s, c, _ := newLobbyProbeServer(t, true)
	seq := loginSequence(t, s, c)
	league, lobby := indexOf(seq, 180), indexOf(seq, 153)
	if league < 0 || lobby < 0 {
		t.Fatalf("missing 180/153 in %v", seq)
	}
	if league < lobby {
		t.Fatalf("180 sent at %d, before 153 at %d: %v", league, lobby, seq)
	}
}

// The reference login sends one notice per configured notice plus the
// hard-coded EN welcome plus the configured welcome message, and the notices go
// out before the shop update (manager/usermanager.cpp:611-622).
func TestReview83LoginNoticeSet(t *testing.T) {
	s, c, _ := newLobbyProbeServer(t, true)
	if err := s.loginPackets(c); err != nil {
		t.Fatal(err)
	}
	seen := drain(c)
	if got := countOf(ids(seen), 67); got != 3 {
		t.Fatalf("login sent %d UMsg packets, reference sends 3 (notice + fixed EN + config welcome): %v", got, ids(seen))
	}
	found := false
	noticeIndex := -1
	for i, msg := range seen {
		if msg.id != 67 || len(msg.data) == 0 {
			continue
		}
		r := &protocol.Reader{Data: msg.data}
		kind := r.U8()
		text := ""
		if kind == 10 {
			text = r.CString()
		} else {
			// Notice frame: id+10000, type, url, start, end, name, description.
			r.U16()
			r.U8()
			r.CString()
			r.U32()
			r.U32()
			text = r.CString()
		}
		if kind == 10 && text == baselineLoginWelcome {
			found = true
		}
		if kind == 65 && text == "Server information" {
			noticeIndex = i
		}
	}
	if !found {
		t.Fatalf("no hard-coded EN welcome msg box in the login chain: %v", ids(seen))
	}
	if shop := firstIndex(seen, 72); noticeIndex < 0 || noticeIndex > shop {
		t.Fatalf("configured notice at %d, shop update at %d; reference sends the notices first", noticeIndex, shop)
	}
}

// Each 153 list entry carries the literal "test_lobby" in the reference
// (SendLobbyJoin, manager/packetmanager.cpp:1756), and the user-join broadcast
// carries "TIBE YEBAT'?" (SendLobbyUserJoin, :1770). The rewrite sends names.
func TestReview83LobbyEntryLabel(t *testing.T) {
	s, c, _ := newLobbyProbeServer(t, true)
	other := &client{
		user: store.User{
			ID:   2,
			Name: "otheruser",
		},
		character: store.Character{
			ID:   2,
			Name: "OtherUser",
		},
		queue:   make(chan outbound, 256),
		inLobby: true,
	}
	s.clients = map[*client]bool{c: true, other: true}
	s.users = map[int]*client{1: c, 2: other}
	if err := s.loginPackets(c); err != nil {
		t.Fatal(err)
	}
	seen := drain(c)
	var list []byte
	for _, msg := range seen {
		if msg.id == 153 && len(msg.data) > 0 && msg.data[0] == 0 {
			list = msg.data
		}
	}
	if list == nil {
		t.Fatalf("no 153/0 in %v", ids(seen))
	}
	r := &protocol.Reader{Data: list}
	r.U8()
	if n := r.U16(); n != 2 {
		t.Fatalf("lobby list count %d, want 2", n)
	}
	r.U32()
	if label := r.CString(); label != "test_lobby" {
		t.Fatalf("153 entry label %q, reference writes %q", label, "test_lobby")
	}
	joined := drain(other)
	for _, msg := range joined {
		if msg.id != 153 || len(msg.data) == 0 || msg.data[0] != 1 {
			continue
		}
		jr := &protocol.Reader{Data: msg.data}
		jr.U8()
		jr.U32()
		if got := jr.CString(); got != "TIBE YEBAT'?" {
			t.Fatalf("153 user-join label %q, reference writes %q", got, "TIBE YEBAT'?")
		}
		return
	}
	t.Fatalf("no 153/1 join broadcast to the other lobby user: %v", ids(joined))
}

// 157 is built with UFLAG_LOW_ALL|UFLAG_HIGH_ALL in the reference, so both flag
// words are -1 and every optional section is present
// (packet/packethelper_fulluserinfo.cpp:43-294).
func TestReview83UserInfoFlags(t *testing.T) {
	s, c, _ := newLobbyProbeServer(t, true)
	if err := s.loginPackets(c); err != nil {
		t.Fatal(err)
	}
	seen := drain(c)
	var info []byte
	for _, msg := range seen {
		if msg.id == 157 {
			info = msg.data
			break
		}
	}
	if info == nil {
		t.Fatalf("no 157 in %v", ids(seen))
	}
	r := &protocol.Reader{Data: info}
	r.U32()
	low := r.U32()
	high := r.U32()
	if low != 0xFFFFFFFF || high != 0xFFFFFFFF {
		t.Fatalf("157 flags low=0x%08x high=0x%08x, reference writes 0xffffffff/0xffffffff (%d bytes)", low, high, len(info))
	}
}

// The lobby user list is built from a map, so its order changes between calls;
// the reference keeps the channel's user vector in join order
// (channel/channel.cpp:57, manager/packetmanager.cpp:1753).
func TestReview83LobbyListOrder(t *testing.T) {
	s, c, _ := newLoginExtraServer(t)
	s.clients = map[*client]bool{}
	c.queue = make(chan outbound, 4096)
	c.inLobby = false
	names := []string{"Alpha", "Bravo", "Delta"}
	users := make([]*client, 0, len(names))
	for i, name := range names {
		u := &client{
			user: store.User{
				ID:   i + 1,
				Name: name,
			},
			character: store.Character{
				ID:   i + 1,
				Name: name,
			},
			queue:   make(chan outbound, 4096),
			inLobby: true,
		}
		s.clients[u] = true
		users = append(users, u)
	}
	blockWidth := func(ch store.Character) int {
		w := &protocol.Writer{}
		userInfo(w, ch)
		return len(w.Data)
	}(users[0].character)
	orders := map[string]int{}
	for i := 0; i < 200; i++ {
		c.inLobby = false
		s.joinLobby(c)
		var list []byte
		for _, msg := range drain(c) {
			if msg.id == 153 && len(msg.data) > 0 && msg.data[0] == 0 {
				list = msg.data
			}
		}
		if list == nil {
			t.Fatal("no 153/0 packet")
		}
		r := &protocol.Reader{Data: list}
		r.U8()
		n := r.U16()
		var order []int
		for range n {
			id := r.U32()
			r.CString()
			r.Bytes(blockWidth)
			order = append(order, id)
			if r.Err != nil {
				t.Fatal(r.Err)
			}
		}
		key := ""
		for _, id := range order {
			key += string(rune('0' + id))
		}
		orders[key]++
	}
	if len(orders) != 1 {
		keys := make([]string, 0, len(orders))
		for key := range orders {
			keys = append(keys, key)
		}
		t.Fatalf("lobby list order is not stable across 200 calls: %d distinct orders %v (reference keeps join order 123)", len(orders), keys)
	}
	for key := range orders {
		if key != "123" {
			t.Fatalf("lobby list order %q, reference keeps join order 123", key)
		}
	}
}

// When an update is refused the reference re-sends the complete current
// settings with an empty change mask so the host's dialog reverts
// (manager/channelmanager.cpp:1714 -> SendRoomUpdateSettings with mask 0 ->
// WriteSettings falls back to the room's own (ALL) flags).
func TestReview83RejectedSettingsRevert(t *testing.T) {
	s, c, _ := newRoomProbeServer(t)
	createRoom(t, s, c)
	drain(c)
	s.revertRoomSettings(s.rooms[1], c)
	var revert []byte
	for _, msg := range drain(c) {
		if msg.id == 65 && len(msg.data) > 0 && msg.data[0] == 4 {
			revert = msg.data
		}
	}
	if revert == nil {
		t.Fatal("no 65/4 revert packet")
	}
	r := &protocol.Reader{Data: revert}
	r.U8()
	flags := [4]uint32{uint32(r.U32()), uint32(r.U32()), uint32(r.U32()), uint32(r.U32())}
	if flags[0] == 0 && flags[1] == 0 && flags[2] == 0 && flags[3] == 0 {
		t.Fatalf("revert sends an empty change mask (%d bytes); reference re-sends the full settings (192 bytes for this room)", len(revert))
	}
}

// Joining a room makes the reference broadcast SetUserTeam (65/13) to the
// existing members after PlayerJoin (room/room.cpp:1378-1388).
func TestReview83RoomJoinTeamPacket(t *testing.T) {
	s, host, db := newRoomProbeServer(t)
	createRoom(t, s, host)
	drain(host)
	if err := db.Register("guestuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCharacter(2, "GuestUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	character, err := db.Character(2)
	if err != nil {
		t.Fatal(err)
	}
	guest := &client{
		user: store.User{
			ID:   2,
			Name: "guestuser",
		},
		character: character,
		queue:     make(chan outbound, 1024),
		inLobby:   true,
		conn:      stubConn{},
	}
	s.clients[guest] = true
	s.users[2] = guest
	w := &protocol.Writer{}
	w.U8(23)
	w.U8(0)
	w.U16(1)
	w.String("")
	if err := s.roomRequest(guest, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	var kinds []byte
	for _, msg := range roomPackets(drain(host)) {
		kinds = append(kinds, msg.data[0])
	}
	if len(kinds) != 2 || kinds[0] != 1 || kinds[1] != 13 {
		t.Fatalf("existing member received 65 subtypes %v; reference sends PlayerJoin then SetUserTeam [1 13]", kinds)
	}
}

// newRoomProbeServer builds a server from the shipped data set so room settings
// defaults and the map/mode tables are real, then returns a lobby client.
func newRoomProbeServer(t *testing.T) (*Server, *client, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(dir, "ServerConfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Room.ValidateSettings = false
	cfg.Crypt = false
	db, err := store.Open(filepath.Join(dir, "UserDatabase.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("roomuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "RoomUser", cfg.DefaultUser); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir, cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	character, err := db.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	c := &client{
		user: store.User{
			ID:   1,
			Name: "roomuser",
		},
		character: character,
		queue:     make(chan outbound, 1024),
		inLobby:   true,
		conn:      stubConn{},
	}
	s.clients[c] = true
	s.users[1] = c
	return s, c, db
}

// roomCreatePayload is the 65/0 body a plain client sends: room name, empty
// password, mode 0, map 0, 16 players, win limit 10, kill limit 100.
func roomCreatePayload(name string) []byte {
	w := &protocol.Writer{}
	w.U8(0)
	w.U32(1<<0 | 1<<3 | 1<<6 | 1<<7 | 1<<8 | 1<<9 | 1<<10)
	w.U32(0)
	w.U32(0)
	w.U32(0)
	w.String(name)
	w.String("")
	w.U8(0)
	w.U16(0)
	w.U8(16)
	w.U8(10)
	w.U16(100)
	return w.Data
}

func createRoom(t *testing.T, s *Server, c *client) {
	t.Helper()
	if err := s.roomRequest(c, &protocol.Reader{Data: roomCreatePayload("Review83Room")}); err != nil {
		t.Fatal(err)
	}
}

func roomPackets(seen []outbound) []outbound {
	var rooms []outbound
	for _, msg := range seen {
		if msg.id == 65 && len(msg.data) > 0 {
			rooms = append(rooms, msg)
		}
	}
	return rooms
}

// Creating a room makes the reference send SetHost, the two host-item settings
// updates and the CreateAndJoin packet (room/room.cpp:94-104 UpdateHost ->
// :969, :899 CheckForHostItems; manager/channelmanager.cpp:1247).
func TestReview83RoomCreatePacketSet(t *testing.T) {
	s, c, _ := newRoomProbeServer(t)
	createRoom(t, s, c)
	rooms := roomPackets(drain(c))
	var got []byte
	var createLen int
	for _, msg := range rooms {
		got = append(got, msg.data[0])
		if msg.data[0] == 0 {
			createLen = len(msg.data)
		}
	}
	want := []byte{5, 4, 4, 0}
	if len(got) != len(want) {
		t.Fatalf("room create sent 65 subtypes %v; reference sends SetHost, superRoom, c4Timer, CreateAndJoin %v (65/0 len %d)", got, want, createLen)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("room create sent 65 subtypes %v; reference sends %v", got, want)
		}
	}
}

// LoadDefaultSettings sets every settings flag to ALL, so the CreateAndJoin
// settings carry every field (room/roomsettings.cpp:1210-1215). Only the Voxel
// block bit is cleared for a non-Studio room.
func TestReview83RoomCreateSettingsFlags(t *testing.T) {
	s, c, _ := newRoomProbeServer(t)
	createRoom(t, s, c)
	var create []byte
	for _, msg := range roomPackets(drain(c)) {
		if msg.data[0] == 0 {
			create = msg.data
		}
	}
	if create == nil {
		t.Fatal("no 65/0 packet")
	}
	r := &protocol.Reader{Data: create}
	r.Bytes(13)
	flags := [4]uint32{uint32(r.U32()), uint32(r.U32()), uint32(r.U32()), uint32(r.U32())}
	want := [4]uint32{0xFFFFFFFF, 0xFFFFFFFF &^ (1 << 18), 0xFFFFFFFF, 0xFFFFFFFF}
	if flags != want {
		t.Fatalf("65/0 settings flags %#x, reference writes %#x (%d bytes)", flags, want, len(create))
	}
}

// The room list entry is built with RLFLAG_ALL|RLHFLAG_ALL, so every field is
// present (manager/packetmanager.cpp:1996 -> BuildRoomInfo:1791-1976).
func TestReview83RoomListEntryFields(t *testing.T) {
	s, c, _ := newRoomProbeServer(t)
	createRoom(t, s, c)
	drain(c)
	s.roomList(c)
	var list []byte
	for _, msg := range drain(c) {
		if msg.id == 151 {
			list = msg.data
		}
	}
	if list == nil {
		t.Fatal("no 151 packet")
	}
	r := &protocol.Reader{Data: list}
	r.U8()
	r.U8()
	r.U16()
	r.U16()
	r.U16()
	r.U16()
	r.U16()
	r.U16()
	if n := r.U8(); n != 1 {
		t.Fatalf("room count %d, want 1", n)
	}
	r.U8()
	r.U32()
	r.U8()
	r.U8()
	r.U16()
	low := r.U32()
	high := r.U32()
	fields := len(list) - r.Pos
	if low != -1 || high != -1 {
		t.Fatalf("151 entry flags low=0x%08x high=0x%08x with %d field bytes; reference writes 0xffffffff/0xffffffff and 85 bytes for this room", low, high, fields)
	}
	if fields != 85 {
		t.Fatalf("151 entry has %d field bytes, reference writes 85 for this room", fields)
	}
}
