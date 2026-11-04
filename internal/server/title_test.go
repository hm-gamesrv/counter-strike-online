package server

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func newTitleTestServer(t *testing.T) (*Server, *client, *store.Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "title.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("titleuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "TitleUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("UPDATE UserCharacter SET achievementList='7,42' WHERE userID=1"); err != nil {
		t.Fatal(err)
	}
	character, err := db.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	c := &client{user: store.User{ID: 1}, character: character, queue: make(chan outbound, 16)}
	return s, c, db
}

func TestTitlePrefixAndList(t *testing.T) {
	s, c, db := newTitleTestServer(t)
	if err := s.dispatch(c, protocol.Packet{ID: 84, Data: []byte{0, 7, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 157 {
		t.Fatalf("missing prefix update, got %d", msg.id)
	}
	if c.character.PrefixID != 7 {
		t.Fatalf("in-memory prefix %d", c.character.PrefixID)
	}
	var prefix int
	if err := db.DB.QueryRow("SELECT prefixID FROM UserCharacter WHERE userID=1").Scan(&prefix); err != nil {
		t.Fatal(err)
	}
	if prefix != 7 {
		t.Fatalf("stored prefix %d", prefix)
	}

	// A title missing from the achievement list must be rejected silently.
	if err := s.dispatch(c, protocol.Packet{ID: 84, Data: []byte{0, 9, 0}}); err != nil {
		t.Fatal(err)
	}
	if len(c.queue) != 0 {
		t.Fatalf("locked prefix produced %d packets", len(c.queue))
	}
	if err := db.DB.QueryRow("SELECT prefixID FROM UserCharacter WHERE userID=1").Scan(&prefix); err != nil {
		t.Fatal(err)
	}
	if prefix != 7 {
		t.Fatalf("locked prefix changed stored value to %d", prefix)
	}

	if err := s.dispatch(c, protocol.Packet{ID: 84, Data: []byte{1, 2, 42, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 157 {
		t.Fatalf("missing title list update, got %d", msg.id)
	}
	if c.character.Titles[2] != 42 {
		t.Fatalf("in-memory title slot %d", c.character.Titles[2])
	}
	var titles string
	if err := db.DB.QueryRow("SELECT titles FROM UserCharacter WHERE userID=1").Scan(&titles); err != nil {
		t.Fatal(err)
	}
	if titles != "0,0,42,0,0" {
		t.Fatalf("stored titles %q", titles)
	}

	// Out-of-range slots are ignored.
	if err := s.dispatch(c, protocol.Packet{ID: 84, Data: []byte{1, 5, 42, 0}}); err != nil {
		t.Fatal(err)
	}
	if len(c.queue) != 0 {
		t.Fatalf("out-of-range slot produced %d packets", len(c.queue))
	}
	if err := db.DB.QueryRow("SELECT titles FROM UserCharacter WHERE userID=1").Scan(&titles); err != nil {
		t.Fatal(err)
	}
	if titles != "0,0,42,0,0" {
		t.Fatalf("out-of-range slot changed titles to %q", titles)
	}

	if err := s.dispatch(c, protocol.Packet{ID: 84, Data: []byte{2, 2}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 157 {
		t.Fatalf("missing title removal update, got %d", msg.id)
	}
	if err := db.DB.QueryRow("SELECT titles FROM UserCharacter WHERE userID=1").Scan(&titles); err != nil {
		t.Fatal(err)
	}
	if titles != "0,0,0,0,0" {
		t.Fatalf("titles after removal %q", titles)
	}

	if err := s.dispatch(c, protocol.Packet{ID: 84, Data: []byte{0, 7, 0, 0}}); err == nil {
		t.Fatal("expected trailing-byte error")
	}
}

func TestUserInfoCarriesTitles(t *testing.T) {
	character := store.Character{
		ID:            1,
		Name:          "TitleUser",
		Level:         3,
		PrefixID:      7,
		Titles:        [store.TitleSlots]int{0, 42, 0, 0, 7},
		Achievements:  []int{7, 42},
		MileagePoints: 1234,
		ChatColorID:   5,
	}
	w := &protocol.Writer{}
	userInfo(w, character)
	fields := parseUserInfoBody(t, w.Data)
	if fields.GameName != "TitleUser" || fields.GameName2 != "TitleUser" {
		t.Fatalf("name mismatch: %q/%q", fields.GameName, fields.GameName2)
	}
	if fields.PrefixID != 7 {
		t.Fatalf("prefix %d", fields.PrefixID)
	}
	if fields.Bitmap[7/8]&(1<<(7%8)) == 0 || fields.Bitmap[42/8]&(1<<(42%8)) == 0 {
		t.Fatal("achievement bitmap missing unlocked titles")
	}
	if want := [5]int{0, 42, 0, 0, 7}; fields.Titles != want {
		t.Fatalf("title slots %v, want %v", fields.Titles, want)
	}
	if fields.Mileage != 1234 {
		t.Fatalf("mileage %d", fields.Mileage)
	}
	if fields.CharacterID != 1 {
		t.Fatalf("id %d", fields.CharacterID)
	}
	if fields.ChatColorID != 5 {
		t.Fatalf("chat color %d", fields.ChatColorID)
	}
	if fields.Consumed != len(w.Data) {
		t.Fatalf("user info tail: consumed %d of %d", fields.Consumed, len(w.Data))
	}
}
