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
	// A selected item (status=1) is refused by CanUseItem: the reference consumes
	// nothing, but still answers the host with the item-used notification.
	if err = s.dispatch(host, protocol.Packet{ID: 68, Data: []byte{100, 2, 0, 0, 0, 0x89, 0x13, 1, 0}}); err != nil {
		t.Fatal(err)
	}
	if len(target.queue) != 0 {
		t.Fatalf("refused item produced an inventory update: %d", len(target.queue))
	}
	reply := <-host.queue
	if reply.id != 68 {
		t.Fatalf("host reply id %d", reply.id)
	}
	rp := &protocol.Reader{Data: reply.data}
	if rp.U8() != 100 || rp.U32() != 2 || rp.U16() != 5001 || rp.U8() != 255 {
		t.Fatalf("unexpected host reply %v: %v", reply.data, rp.Err)
	}
	var count, status int
	if err = db.DB.QueryRow("SELECT count,status FROM UserInventory WHERE userID=2 AND slot=5").Scan(&count, &status); err != nil {
		t.Fatal(err)
	}
	if count != 2 || status != 1 {
		t.Fatalf("refused item changed: count=%d status=%d", count, status)
	}

	// An unusable item that is not selected takes the activation branch of
	// UseItem: it becomes selected and the client gets the new state.
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(2,6,5002,1,0,0,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	s.items = map[int]itemDefinition{5002: {Category: 2}}
	if err = s.dispatch(host, protocol.Packet{ID: 68, Data: []byte{100, 2, 0, 0, 0, 0x8A, 0x13, 1, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-target.queue; msg.id != 154 {
		t.Fatalf("activation update id %d", msg.id)
	}
	if err = db.DB.QueryRow("SELECT count,status FROM UserInventory WHERE userID=2 AND slot=6").Scan(&count, &status); err != nil {
		t.Fatal(err)
	}
	if count != 1 || status != 1 {
		t.Fatalf("activation did not mark the item used: count=%d status=%d", count, status)
	}
	if reply = <-host.queue; reply.id != 68 {
		t.Fatalf("missing host reply for the activation case: %d", reply.id)
	}

	// Unknown item id: no inventory update and no host reply.
	if err = s.dispatch(host, protocol.Packet{ID: 68, Data: []byte{100, 2, 0, 0, 0, 0x8B, 0x13, 1, 0}}); err != nil {
		t.Fatal(err)
	}
	if len(host.queue) != 0 || len(target.queue) != 0 {
		t.Fatalf("unknown item produced packets %d/%d", len(host.queue), len(target.queue))
	}
}
func TestHostDropBoxGrantsReward(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "dropbox.db3"))
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
	host := &client{
		user: store.User{
			ID: 1,
		},
		character: store.Character{
			ID:   1,
			Name: "HostUser",
		},
		version: true,
		room:    1,
		queue:   make(chan outbound, 16),
	}
	target := &client{
		user: store.User{
			ID: 2,
		},
		character: store.Character{
			ID:   2,
			Name: "TargetUser",
		},
		version: true,
		room:    1,
		queue:   make(chan outbound, 16),
	}
	match := &gameMatch{
		id:    "match",
		stats: map[int]*matchStat{2: {user: target}},
	}
	currentRoom := &room{
		id:      1,
		host:    host,
		members: []*client{host, target},
		match:   match,
	}
	s := &Server{
		Store: db,
		Config: config.Config{
			InventorySlotMax: 10,
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		users: map[int]*client{
			1: host,
			2: target,
		},
		rooms: map[int]*room{
			1: currentRoom,
		},
		items: map[int]itemDefinition{
			12: {
				Category: 2,
			},
		},
		rewards: map[int]itemReward{
			3000: {
				Title: "Drop box",
				Items: map[string]struct{ Count, Duration int }{
					"12": {
						Count: 1,
					},
				},
			},
		},
	}
	// Host event 34: drop box pickup with reward 3000.
	if err = s.dispatch(host, protocol.Packet{ID: 68, Data: []byte{11, 34, 2, 0, 0, 0, 0xB8, 0x0B, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-target.queue; msg.id != 154 {
		t.Fatalf("missing drop box inventory update, got %d", msg.id)
	}
	if msg := <-target.queue; msg.id != 67 {
		t.Fatalf("missing drop box reward notice, got %d", msg.id)
	}
	var count int
	if err = db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=2 AND itemID=12").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("drop box item count %d", count)
	}
}
