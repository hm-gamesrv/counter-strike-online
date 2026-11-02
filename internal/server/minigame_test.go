package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func weaponReleaseServer(t *testing.T, active bool) (*Server, *client, int) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "release.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("release", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	var id int
	if err = db.DB.QueryRow("SELECT userID FROM User WHERE userName='release'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(id, "Release", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	for letter, count := range map[string]int{"G": 1, "A": 1, "L": 2, "I": 1} {
		if _, err = db.DB.Exec("INSERT INTO UserMiniGameWeaponReleaseCharacters(userID,character,count) VALUES(?,?,?)", id, letter, count); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{
		Store: db,
		Config: config.Config{
			InventorySlotMax: 3000,
		},
		items: map[int]itemDefinition{
			567: {
				Category: 1,
			},
		},
		miniGame: weaponReleaseConfig{
			Active: active,
			Rows: []weaponReleaseRow{
				{
					ItemID:   567,
					Count:    1,
					Duration: 30,
					Name:     "GALIL",
				},
				{
					ItemID: 566,
					Count:  1,
					Name:   "DAGGER",
				},
			},
			Characters: []int{'G', 'A', 'L', 'I', 'D', 'E', 'R'},
		},
		clients: map[*client]bool{},
		users:   map[int]*client{},
	}
	c := &client{
		user: store.User{
			ID:   id,
			Name: "release",
		},
		character: store.Character{
			ID:   id,
			Name: "Release",
		},
		queue: make(chan outbound, 64),
	}
	s.clients[c] = true
	s.users[id] = c
	return s, c, id
}

func weaponReleaseDispatch(t *testing.T, s *Server, c *client, data []byte) []outbound {
	t.Helper()
	if err := s.dispatch(c, protocol.Packet{ID: 89, Data: data}); err != nil {
		t.Fatal(err)
	}
	var out []outbound
	for {
		select {
		case message := <-c.queue:
			out = append(out, message)
		default:
			return out
		}
	}
}

func TestWeaponReleaseUpdatePacket(t *testing.T) {
	s, c, _ := weaponReleaseServer(t, true)
	messages := weaponReleaseDispatch(t, s, c, []byte{6, 60})
	if len(messages) != 1 {
		t.Fatalf("update messages %d", len(messages))
	}
	r := &protocol.Reader{Data: messages[0].data}
	if messages[0].id != 89 || r.U8() != 6 || r.U8() != 65 || r.U8() != 1 || r.U8() != 2 {
		t.Fatalf("update header %v", messages[0].data)
	}
	if r.U16() != 567 || r.U8() != 1 || r.U8() != 30 || r.CString() != "GALIL" {
		t.Fatalf("first row %v", messages[0].data)
	}
	if r.U8() != 0 || r.U8() != 0 {
		t.Fatal("unexpected progress")
	}
	if r.U16() != 566 || r.U8() != 1 || r.U8() != 0 || r.CString() != "DAGGER" {
		t.Fatalf("second row %v", messages[0].data)
	}
	if r.U8() != 0 || r.U8() != 0 {
		t.Fatal("unexpected second row progress")
	}
	if r.U8() != len(s.miniGame.Characters) {
		t.Fatal("character count")
	}
	for range s.miniGame.Characters {
		r.U8()
		r.U8()
	}
	if r.U16() != 5 {
		t.Fatalf("total letters %v", messages[0].data)
	}
}

func TestWeaponReleaseSetAndReward(t *testing.T) {
	s, c, id := weaponReleaseServer(t, true)
	steps := []struct {
		slot int
		char byte
	}{
		{0, 'L'},
		{1, 'I'},
		{2, 'L'},
		{3, 'A'},
		{4, 'G'},
	}
	for i, step := range steps {
		messages := weaponReleaseDispatch(t, s, c, []byte{6, 61, 0, byte(step.slot), step.char})
		var reply outbound
		for _, message := range messages {
			r := &protocol.Reader{Data: message.data}
			if message.id == 89 && r.U8() == 6 && r.U8() == 66 {
				reply = message
				break
			}
		}
		if reply.id != 89 {
			t.Fatalf("missing set reply %d", i)
		}
		r := &protocol.Reader{Data: reply.data}
		r.U8()
		r.U8()
		status := r.U8()
		wantStatus := 0
		if i == len(steps)-1 {
			wantStatus = 2
		}
		if status != wantStatus {
			t.Fatalf("step %d status %d, want %d", i, status, wantStatus)
		}
		if status <= 2 {
			if r.U8() != boolInt(status != 2) || r.U8() != 0 || r.U8() != step.slot || r.U8() != int(step.char) {
				t.Fatalf("step %d payload %v", i, reply.data)
			}
		}
	}
	items, err := s.Store.Inventory(id)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range items {
		if item.ID == 567 {
			found = true
		}
	}
	if !found {
		t.Fatal("weapon reward missing")
	}
	rows, err := s.Store.WeaponReleaseRows(id)
	if err != nil || len(rows) != 1 || rows[0].Opened != 1 {
		t.Fatalf("progress rows %+v, %v", rows, err)
	}
}

func TestWeaponReleaseInactiveAndDuplicate(t *testing.T) {
	s, c, _ := weaponReleaseServer(t, false)
	messages := weaponReleaseDispatch(t, s, c, []byte{6, 61, 0, 0, 'L'})
	r := &protocol.Reader{Data: messages[0].data}
	if messages[0].id != 89 || r.U8() != 6 || r.U8() != 66 || r.U8() != 3 {
		t.Fatalf("inactive reply %v", messages[0].data)
	}
	s.miniGame.Active = true
	messages = weaponReleaseDispatch(t, s, c, []byte{6, 61, 1, 5, 'D'})
	r = &protocol.Reader{Data: messages[0].data}
	r.U8()
	r.U8()
	if r.U8() != 4 {
		t.Fatalf("missing letter reply %v", messages[0].data)
	}
	messages = weaponReleaseDispatch(t, s, c, []byte{6, 61, 0, 0, 'L'})
	r = &protocol.Reader{Data: messages[0].data}
	r.U8()
	r.U8()
	if r.U8() != 0 {
		t.Fatalf("first set reply %v", messages[0].data)
	}
	messages = weaponReleaseDispatch(t, s, c, []byte{6, 61, 0, 0, 'L'})
	r = &protocol.Reader{Data: messages[0].data}
	r.U8()
	r.U8()
	if r.U8() != 5 {
		t.Fatalf("duplicate reply %v", messages[0].data)
	}
}
func TestLoadWeaponReleaseConfig(t *testing.T) {
	loaded, err := loadWeaponRelease(config.MiniGameWeaponRelease{
		Active: true,
		Items: config.MiniGameItems{
			{
				ID:       567,
				Name:     "GALIL",
				Count:    1,
				Duration: 30,
			},
		},
		Characters: []string{"G", "~"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Active || len(loaded.Rows) != 1 || len(loaded.Characters) != 2 || loaded.Rows[0].ItemID != 567 {
		t.Fatalf("loaded config %+v", loaded)
	}
	if _, err = loadWeaponRelease(config.MiniGameWeaponRelease{
		Active:     true,
		Items:      config.MiniGameItems{{ID: 567, Name: "TOO-LONG-NAME", Count: 1}},
		Characters: []string{"G"},
	}); err == nil {
		t.Fatal("long row name accepted")
	}
	if _, err = loadWeaponRelease(config.MiniGameWeaponRelease{
		Active:     true,
		Items:      config.MiniGameItems{{ID: 567, Name: "GALIL", Count: 1}},
		Characters: []string{"GG"},
	}); err == nil {
		t.Fatal("multi-byte character accepted")
	}
}
