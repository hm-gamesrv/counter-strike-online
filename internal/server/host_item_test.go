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

func TestHostScenarioItemUse(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "hostitem.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("hostuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "HostUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if err = db.Register("targetuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(2, "TargetUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(2,5,5001,2,1,0,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	host := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "HostUser"}, version: true, room: 1, queue: make(chan outbound, 16)}
	target := &client{user: store.User{ID: 2}, character: store.Character{ID: 2, Name: "TargetUser"}, version: true, room: 1, queue: make(chan outbound, 16)}
	match := &gameMatch{id: "match", stats: map[int]*matchStat{2: {user: target}}}
	r := &room{id: 1, host: host, members: []*client{host, target}, match: match}
	s := &Server{
		Store:  db,
		Config: config.Config{},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		users:  map[int]*client{1: host, 2: target},
		rooms:  map[int]*room{1: r},
	}
	if err = s.dispatch(host, protocol.Packet{ID: 68, Data: []byte{100, 2, 0, 0, 0, 0x89, 0x13, 1, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-target.queue; msg.id != 154 {
		t.Fatalf("target inventory update %d", msg.id)
	}
	reply := <-host.queue
	if reply.id != 68 {
		t.Fatalf("host reply id %d", reply.id)
	}
	rp := &protocol.Reader{Data: reply.data}
	if rp.U8() != 100 || rp.U32() != 2 || rp.U16() != 5001 || rp.U8() != 255 {
		t.Fatalf("unexpected host reply %v: %v", reply.data, rp.Err)
	}
	var count int
	if err = db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=2 AND slot=5").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("scenario item count %d", count)
	}

	// Unknown item: no inventory update and no host reply.
	if err = s.dispatch(host, protocol.Packet{ID: 68, Data: []byte{100, 2, 0, 0, 0, 0x8A, 0x13, 1, 0}}); err != nil {
		t.Fatal(err)
	}
	if len(host.queue) != 0 || len(target.queue) != 0 {
		t.Fatalf("unknown item produced packets %d/%d", len(host.queue), len(target.queue))
	}
}
