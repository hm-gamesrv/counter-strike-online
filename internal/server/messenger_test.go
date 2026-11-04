package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestMessengerReturnsTargetProfile(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "messenger.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"sender", "target"} {
		if err = db.Register(name, "secret1", "127.0.0.1", 0); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.CreateCharacter(1, "Sender", config.DefaultUser{Level: 3}); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(2, "Target", config.DefaultUser{Level: 7}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "Sender"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 93, Data: []byte{1, 'T', 'a', 'r', 'g', 'e', 't', 0}}); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	r := &protocol.Reader{Data: msg.data}
	if msg.id != 93 || r.U8() != 1 || r.U32() != 2 || r.CString() != "22223123132" {
		t.Fatalf("unexpected messenger header %v", msg.data)
	}
	if name := parseUserInfoBody(t, msg.data[r.Pos:]).GameName; name != "Target" {
		t.Fatalf("messenger name %q", name)
	}
	if err = s.dispatch(c, protocol.Packet{ID: 93, Data: []byte{1, 'M', 'i', 's', 's', 'i', 'n', 'g', 0}}); err != nil {
		t.Fatal(err)
	}
	msg = <-c.queue
	r = &protocol.Reader{Data: msg.data}
	if msg.id != 93 || r.U8() != 1 || r.U32() != 0 || r.CString() != "22223123132" {
		t.Fatalf("unexpected missing-user header %v", msg.data)
	}
	if name := parseUserInfoBody(t, msg.data[r.Pos:]).GameName; name != "Missing" {
		t.Fatalf("missing-user name %q", name)
	}
}
