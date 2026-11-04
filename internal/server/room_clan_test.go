package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func familyBattleSetup(t *testing.T) (*Server, *client, *client, *client, *client) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "family.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, name := range []string{"alpha", "bravo", "charlie", "delta"} {
		if err = db.Register(name, "secret1", "127.0.0.1", 0); err != nil {
			t.Fatal(err)
		}
		if err = db.CreateCharacter(map[string]int{"alpha": 1, "bravo": 2, "charlie": 3, "delta": 4}[name], name, config.DefaultUser{Level: 1, Points: 40000}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := db.CreateClan(store.ClanCreate{MasterID: 1, Name: "First", JoinMethod: 3}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateClan(store.ClanCreate{MasterID: 2, Name: "Second", JoinMethod: 3}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{4} {
		if code, _, err := db.JoinClan(id, first, 1000); err != nil || code != 1 {
			t.Fatalf("join first %d, %v", code, err)
		}
	}
	_ = second
	makeClient := func(id int, name string) *client {
		character, err := db.Character(id)
		if err != nil {
			t.Fatal(err)
		}
		return &client{
			user: store.User{
				ID:   id,
				Name: name,
			},
			character: character,
			queue:     make(chan outbound, 32),
		}
	}
	a := makeClient(1, "alpha")
	b := makeClient(2, "bravo")
	c := makeClient(3, "charlie")
	d := makeClient(4, "delta")
	s := &Server{
		Store:   db,
		clients: map[*client]bool{a: true, b: true, c: true, d: true},
		users:   map[int]*client{1: a, 2: b, 3: c, 4: d},
	}
	return s, a, b, c, d
}

func familySettings() settings {
	s := settings{}
	s.flags[2] = 1 << 6
	s.fields[70] = []byte{1}
	return s
}

func TestFamilyBattleClanSlotsAndNotice(t *testing.T) {
	s, a, b, c, d := familyBattleSetup(t)
	r := &room{
		id:       7,
		host:     a,
		members:  []*client{a},
		settings: familySettings(),
	}
	// Creation checks membership but leaves both slots empty
	// (src/manager/channelmanager.cpp:1233-1241).
	if !s.familyBattleInit(r, a) || r.clan1 != 0 || r.clan2 != 0 {
		t.Fatalf("creation assigned a clan slot %+v", r)
	}
	// The first joiner fills slot two whatever clan it belongs to (:1340-1352),
	// which is what lets a same-clan room start.
	if !s.familyBattleJoin(r, d) || r.clan2 == 0 || r.clan2 != d.character.ClanID {
		t.Fatalf("first joiner did not fill the second slot %+v", r)
	}
	if s.familyBattleJoin(r, b) {
		t.Fatal("a third clan joined")
	}
	if s.familyBattleJoin(r, c) {
		t.Fatal("clanless member joined")
	}
	r.members = append(r.members, d)
	if notice := s.familyBattleStartRefusal(r); notice != "ROOM_START_FAILED_CLAN_NOT_READY" {
		t.Fatalf("a room with no ready player was accepted: %q", notice)
	}
	d.ready = 2
	if notice := s.familyBattleStartRefusal(r); notice != "" {
		t.Fatalf("ready room refused: %q", notice)
	}
	r.members = []*client{a, d}
	s.familyBattleRecompute(r)
	if r.clan2 != 0 || s.familyBattleStartRefusal(r) == "" {
		t.Fatalf("second clan not cleared %+v", r)
	}
	s.rooms = map[int]*room{7: r}
	a.room = 7
	if err := s.roomNoticeClan(a); err != nil {
		t.Fatal(err)
	}
	for _, target := range []*client{a, d} {
		message := <-target.queue
		reader := &protocol.Reader{Data: message.data}
		if message.id != 71 || reader.U8() != 116 || reader.U8() != 1 || reader.CString() != "alpha" || reader.U8() != 0 || reader.U16() != 7 {
			t.Fatalf("clan battle notice %v", message.data)
		}
	}
}
