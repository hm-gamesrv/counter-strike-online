package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func review70Room(t *testing.T) (*Server, *client, *room) {
	t.Helper()
	s, c, _ := newLoginExtraServer(t)
	c.room = 1
	st := validRoomSettings()
	name := &protocol.Writer{}
	name.String("Review70")
	st.flags[0] |= 1
	st.fields[0] = name.Data
	r := &room{
		id:       1,
		host:     c,
		members:  []*client{c},
		settings: st,
	}
	s.rooms = map[int]*room{1: r}
	s.users = map[int]*client{1: c}
	return s, c, r
}

func review70HostItem(t *testing.T, s *Server, itemID, status, inUse int) {
	t.Helper()
	_, err := s.Store.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,?, ?,1,?,?,0,0,0,0,0,0,0,0,0,0)", itemID, itemID, status, inUse)
	if err != nil {
		t.Fatal(err)
	}
}

func review70Migrate(t *testing.T, s *Server, r *room, successor *client) {
	t.Helper()
	old := pipeClient(t, 2, "OldHost")
	old.room = r.id
	r.host = old
	r.members = []*client{old, successor}
	s.users[2] = old
	s.leaveRoom(old)
	if r.host != successor {
		t.Fatal("host did not migrate")
	}
}

func TestReview70MigrationRejectsDormantHostItems(t *testing.T) {
	s, c, r := review70Room(t)
	review70HostItem(t, s, superRoomItemID, 0, 0)
	review70Migrate(t, s, r, c)
	if r.settings.number(superRoomField) != 0 {
		t.Fatalf("dormant item granted superRoom=%d; baseline requires status=1 AND inUse=1", r.settings.number(superRoomField))
	}
}

func TestReview70MigrationEnablesNewHostsC4(t *testing.T) {
	s, c, r := review70Room(t)
	r.settings.setNumber(c4Field, 0, 1)
	review70HostItem(t, s, c4SoundItemID, 1, 1)
	review70Migrate(t, s, r, c)
	if r.settings.number(c4Field) != 1 {
		t.Fatalf("new host has active C4 item but c4Timer=%d", r.settings.number(c4Field))
	}
}

func TestReview70SanitizedVoxelMaskDoesNotCorruptFollowingField(t *testing.T) {
	s, c, r := review70Room(t)
	validation := validateServer()
	s.Config.Room.ValidateSettings = true
	s.maps = validation.maps
	s.modes = validation.modes
	w := &protocol.Writer{}
	w.U8(5)
	w.U32(0)
	w.U32(1<<18 | 1<<22)
	w.U32(0)
	w.U32(0)
	w.U32(1) // VOXELFLAG_ID; normal maps must ignore this block.
	w.String("abc")
	w.U8(1) // teamSwitch, following the variable-size Voxel block.
	if err := s.roomRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	if r.settings.hasField(50) {
		t.Fatal("fixture did not sanitize the Voxel block")
	}
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id != 65 || len(msg.data) == 0 || msg.data[0] != 4 {
			continue
		}
		p := &protocol.Reader{Data: msg.data[1:]}
		decoded, err := parseSettings(p)
		if err != nil {
			t.Fatal(err)
		}
		if decoded.hasField(50) || decoded.number(54) != r.settings.number(54) || p.Pos != len(p.Data) {
			t.Fatalf("sanitized broadcast: voxel=%v teamSwitch=%d stored=%d unread=%d", decoded.hasField(50), decoded.number(54), r.settings.number(54), len(p.Data)-p.Pos)
		}
		return
	}
	t.Fatal("no settings broadcast")
}

func TestReview70FamilyBattleOpponentsJoinOppositeTeams(t *testing.T) {
	s, a, b, _, _ := familyBattleSetup(t)
	a.conn = pipeClient(t, 1, "alpha").conn
	b.conn = pipeClient(t, 2, "bravo").conn
	a.room = 7
	a.team = 2
	b.inLobby = true
	st := familySettings()
	st.setNumber(8, 8, 1)
	r := &room{
		id:       7,
		host:     a,
		members:  []*client{a},
		settings: st,
	}
	if !s.familyBattleInit(r, a) {
		t.Fatal("cannot initialize family battle")
	}
	s.rooms = map[int]*room{7: r}
	w := &protocol.Writer{}
	w.U8(23)
	w.U8(0)
	w.U16(7)
	w.String("")
	if err := s.roomRequest(b, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	if b.room != 7 || r.clan1 == r.clan2 || r.clan2 == 0 {
		t.Fatal("opposing clan did not join")
	}
	if b.team == a.team {
		t.Fatalf("opposing clans joined same team: host=%d guest=%d", a.team, b.team)
	}
}

func TestReview70KillPaysGachaponAtCounterBoundary(t *testing.T) {
	s, c, r := review70Room(t)
	if _, err := s.Store.DB.Exec("UPDATE UserCharacterExtended SET killsToGetGachaponItem=1 WHERE userID=1"); err != nil {
		t.Fatal(err)
	}
	r.match = &gameMatch{
		id: "review70-kill",
		stats: map[int]*matchStat{
			1: {user: c},
		},
	}
	w := &protocol.Writer{}
	w.U8(3)
	w.U32(1)
	w.U16(12)
	w.U8(2)
	w.U32(2)
	w.U8(1)
	w.U16(0)
	w.Bytes(make([]byte, 24))
	if err := s.hostRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	var counter, awards int
	if err := s.Store.DB.QueryRow("SELECT killsToGetGachaponItem FROM UserCharacterExtended WHERE userID=1").Scan(&counter); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.DB.QueryRow("SELECT coalesce(sum(count),0) FROM UserInventory WHERE userID=1 AND itemID IN (166,5101)").Scan(&awards); err != nil {
		t.Fatal(err)
	}
	if counter != 100 || awards != 1 {
		t.Fatalf("kill reward boundary: counter=%d awards=%d; baseline resets to 100 and grants one item", counter, awards)
	}
}
