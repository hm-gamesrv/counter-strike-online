package server

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestServerYellConsumesItemAndBroadcasts(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "yell.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, account := range []struct {
		id      int
		account string
		name    string
	}{{1, "user1", "Yeller"}, {2, "user2", "Listener"}} {
		if err = db.Register(account.account, "secret1", "127.0.0.1", 0); err != nil {
			t.Fatal(err)
		}
		if err = db.CreateCharacter(account.id, account.name, config.DefaultUser{Level: 1}); err != nil {
			t.Fatal(err)
		}
	}
	now := int(time.Now().Unix() / 60)
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,5,204,2,1,0,?,0,0,0,0,0,0,0,0,0)", now); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:  db,
		Config: config.Config{},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	sender := &client{user: store.User{ID: 1, Name: "user1"}, character: store.Character{ID: 1, Name: "Yeller"}, version: true, queue: make(chan outbound, 16)}
	listener := &client{user: store.User{ID: 2, Name: "user2"}, character: store.Character{ID: 2, Name: "Listener"}, version: true, queue: make(chan outbound, 16)}
	s.clients = map[*client]bool{sender: true, listener: true}

	if err = s.dispatch(sender, protocol.Packet{ID: 67, Data: []byte{6, 'h', 'i', 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-sender.queue; msg.id != 154 {
		t.Fatalf("missing inventory update, got %d", msg.id)
	}
	if msg := <-sender.queue; msg.id != 67 {
		t.Fatalf("missing sender echo, got %d", msg.id)
	}
	broadcast := <-listener.queue
	if broadcast.id != 67 {
		t.Fatalf("listener got packet %d", broadcast.id)
	}
	r := &protocol.Reader{Data: broadcast.data}
	if r.U8() != 6 || r.CString() != "Yeller" || r.CString() != "hi" {
		t.Fatalf("unexpected yell packet: %v at %d", r.Err, r.Pos)
	}
	var count int
	if err = db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND slot=5").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("remaining yell items %d", count)
	}

	if err = s.dispatch(sender, protocol.Packet{ID: 67, Data: []byte{6, 'b', 'y', 'e', 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-sender.queue; msg.id != 154 {
		t.Fatalf("missing removal update, got %d", msg.id)
	}
	var rows int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND slot=5").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("used yell item row still present %d", rows)
	}
	if msg := <-listener.queue; msg.id != 67 {
		t.Fatalf("listener missed second yell, got %d", msg.id)
	}
}

func TestServerYellRequiresItemAndRespectsBan(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "yell.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, account := range []struct {
		id      int
		account string
		name    string
	}{{1, "user1", "Yeller"}, {2, "user2", "Listener"}} {
		if err = db.Register(account.account, "secret1", "127.0.0.1", 0); err != nil {
			t.Fatal(err)
		}
		if err = db.CreateCharacter(account.id, account.name, config.DefaultUser{Level: 1}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{Store: db, Config: config.Config{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	sender := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "Yeller"}, version: true, queue: make(chan outbound, 16)}
	listener := &client{user: store.User{ID: 2}, character: store.Character{ID: 2, Name: "Listener"}, version: true, queue: make(chan outbound, 16)}
	s.clients = map[*client]bool{sender: true, listener: true}

	// Without a megaphone nothing is sent and nothing is consumed.
	if err = s.dispatch(sender, protocol.Packet{ID: 67, Data: []byte{6, 'h', 'i', 0}}); err != nil {
		t.Fatal(err)
	}
	if len(sender.queue) != 0 || len(listener.queue) != 0 {
		t.Fatalf("yell without item produced packets %d/%d", len(sender.queue), len(listener.queue))
	}

	now := int(time.Now().Unix() / 60)
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,5,204,1,1,0,?,0,0,0,0,0,0,0,0,0)", now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AddBan(2, "Yeller", 300); err != nil {
		t.Fatal(err)
	}
	if err = db.SetBanSettings(2, 2); err != nil {
		t.Fatal(err)
	}
	if err = s.dispatch(sender, protocol.Packet{ID: 67, Data: []byte{6, 'h', 'i', 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-sender.queue; msg.id != 154 {
		t.Fatalf("sender inventory update missing, got %d", msg.id)
	}
	if msg := <-sender.queue; msg.id != 67 {
		t.Fatalf("sender echo missing, got %d", msg.id)
	}
	if len(listener.queue) != 0 {
		t.Fatalf("banned listener received %d packets", len(listener.queue))
	}
}
