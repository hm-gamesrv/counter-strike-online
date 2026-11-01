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
	r := &protocol.Reader{Data: w.Data}
	flags := r.U32()
	if flags&(1<<19) == 0 || flags&(1<<22) == 0 {
		t.Fatalf("achievement/title flags missing: %#x", flags)
	}
	if high := r.U32(); high != 1 {
		t.Fatalf("high flag %d", high)
	}
	if r.CString() != "TitleUser" {
		t.Fatal("name mismatch")
	}
	r.U32()
	for i := 0; i < 3; i++ {
		r.U32()
		r.U32()
	}
	r.U32()
	r.U32()
	r.U32()
	r.U32()
	for i := 0; i < 16; i++ {
		r.U32()
	}
	r.CString()
	r.U16()
	r.U16()
	r.U16()
	r.CString()
	r.U16()
	if prefix := r.U16(); prefix != 7 {
		t.Fatalf("prefix %d", prefix)
	}
	r.U8()
	r.U8()
	r.U32()
	bitmap := r.Bytes(128)
	if bitmap[7/8]&(1<<(7%8)) == 0 || bitmap[42/8]&(1<<(42%8)) == 0 {
		t.Fatal("achievement bitmap missing unlocked titles")
	}
	for _, want := range []int{0, 42, 0, 0, 7} {
		if got := r.U16(); got != want {
			t.Fatalf("title slot %d, want %d", got, want)
		}
	}
	if mileage := r.U32(); mileage != 1234 {
		t.Fatalf("mileage %d", mileage)
	}
	r.U32()
	if id := r.U32(); id != 1 {
		t.Fatalf("id %d", id)
	}
	if color := r.U16(); color != 5 {
		t.Fatalf("chat color %d", color)
	}
	if r.Err != nil || r.Pos != len(r.Data) {
		t.Fatalf("unexpected user info tail: %v at %d/%d", r.Err, r.Pos, len(r.Data))
	}
}
