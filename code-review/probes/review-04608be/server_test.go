package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestReview046HostUseSelectsActivatedUnselectedItem(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	c.room = 1
	s.users = map[int]*client{1: c}
	s.rooms = map[int]*room{1: {
		id:   1,
		host: c,
		match: &gameMatch{
			id: "host-use",
		},
	}}
	s.items[7001] = itemDefinition{Category: 8}
	if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{{
		ID:         7001,
		Count:      2,
		Unselected: true,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	w := &protocol.Writer{}
	w.U8(100)
	w.U32(1)
	w.U16(7001)
	w.U16(1)
	if err := s.hostRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	var status, inUse int
	if err := db.DB.QueryRow("SELECT status,inUse FROM UserInventory WHERE userID=1 AND itemID=7001").Scan(&status, &inUse); err != nil {
		t.Fatal(err)
	}
	if status != 1 {
		t.Fatalf("UseItem left status=%d inUse=%d; baseline sets status=1 even when inUse is already 1", status, inUse)
	}
}

func review046Room(t *testing.T) (*Server, *client, *room) {
	t.Helper()
	s, c, _ := newLoginExtraServer(t)
	c.room = 1
	st := settings{}
	name := &protocol.Writer{}
	name.String("ReviewRoom")
	st.flags[0] |= 1
	st.fields[0] = name.Data
	st.setNumber(6, 0, 1)
	st.setNumber(7, 1, 2)
	st.setNumber(8, 8, 1)
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

func TestReview046SettingsBroadcastUsesEffectiveValues(t *testing.T) {
	s, c, room := review046Room(t)
	w := &protocol.Writer{}
	w.U8(5)
	w.U32(0)
	w.U32(1 << 26)
	w.U32(0)
	w.U32(0)
	w.U8(1)
	if err := s.roomRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	if room.settings.number(58) != 0 {
		t.Fatal("fixture did not reject unowned superRoom setting")
	}
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id != 65 || msg.data[0] != 4 {
			continue
		}
		st, err := parseSettings(&protocol.Reader{Data: msg.data[1:]})
		if err != nil {
			t.Fatal(err)
		}
		if st.number(58) != room.settings.number(58) {
			t.Fatalf("broadcast superRoom=%d, stored superRoom=%d", st.number(58), room.settings.number(58))
		}
		return
	}
	t.Fatal("no settings broadcast")
}

func TestReview046HostMigrationRefreshesHostItems(t *testing.T) {
	s, successor, room := review046Room(t)
	old := pipeClient(t, 2, "OldHost")
	old.room = 1
	room.host = old
	room.members = []*client{old, successor}
	room.settings.setNumber(58, 1, 1)
	room.settings.setNumber(33, 1, 1)
	s.users[2] = old
	s.leaveRoom(old)
	if room.host != successor {
		t.Fatal("host was not migrated")
	}
	if room.settings.number(58) != 0 || room.settings.number(33) != 0 {
		t.Fatalf("new host owns no host items, but superRoom=%d c4Timer=%d survived", room.settings.number(58), room.settings.number(33))
	}
}
