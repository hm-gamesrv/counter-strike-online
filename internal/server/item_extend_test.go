package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestExtendItemConsumesExtenderAndUpdatesExpiry(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "extend.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("extender", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Extender", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	now := int(time.Now().Unix() / 60)
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,14,1,1,1,?,?,0,0,0,0,0,0,0,0)", now, now+1000); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,3,676,5,1,1,?,0,0,0,0,0,0,0,0,0)", now); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{InventorySlotMax: 10}}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "Extender"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{16, 0, 2, 0, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing extension inventory update, got %d", msg.id)
	}
	var expiry, count int
	if err = db.DB.QueryRow("SELECT expiryDate FROM UserInventory WHERE userID=1 AND slot=2").Scan(&expiry); err != nil {
		t.Fatal(err)
	}
	if expiry != now+1000+3*1440 {
		t.Fatalf("extended expiry %d, want %d", expiry, now+1000+3*1440)
	}
	if err = db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND slot=3").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("extender count %d, want 2", count)
	}
}

func TestExtendItemRejectsPermanentTarget(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "extend.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("extender", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Extender", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,14,1,1,1,?,0,0,0,0,0,0,0,0,0)", time.Now().Unix()/60); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,3,676,5,1,1,?,0,0,0,0,0,0,0,0,0)", time.Now().Unix()/60); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{InventorySlotMax: 10}}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "Extender"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{16, 0, 2, 0, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	r := &protocol.Reader{Data: msg.data}
	if msg.id != 67 || r.U8() != 10 || r.CString() != "Item unavailable." {
		t.Fatalf("unexpected permanent-target reply %v", msg.data)
	}
	var count int
	if err = db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND slot=3").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 5 {
		t.Fatalf("permanent target consumed extender, count %d", count)
	}
}
