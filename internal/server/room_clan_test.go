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
	if !s.familyBattleInit(r, a) || r.clan1 == 0 {
		t.Fatal("host clan was not assigned")
	}
	if !s.familyBattleJoin(r, d) || r.clan2 != 0 {
		t.Fatal("same-clan member changed the second slot")
	}
	if !s.familyBattleJoin(r, b) || r.clan2 == 0 || r.clan1 == r.clan2 {
		t.Fatalf("second clan %+v", r)
	}
	if s.familyBattleJoin(r, c) {
		t.Fatal("clanless member joined")
	}
	r.members = append(r.members, d, b)
	if !s.familyBattleCanStart(r) {
		t.Fatal("both clans should be able to start")
	}
	r.members = []*client{a, d}
	s.familyBattleRecompute(r)
	if r.clan2 != 0 || s.familyBattleCanStart(r) {
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
