package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestCostumeToggleAndReplacement(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "costume.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("costumeuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "CostumeUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	now := int(time.Now().Unix() / 60)
	for slot, item := range map[int]int{2: 471, 3: 475} {
		if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,?,?,1,1,0,?,0,0,0,0,0,0,0,0,0)", slot, item, now); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{
		Store:  db,
		Config: config.Config{InventorySlotMax: 10},
		items:  map[int]itemDefinition{471: {ClassName: "HeadCostume"}, 475: {ClassName: "HeadCostume"}},
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "CostumeUser"}, queue: make(chan outbound, 16)}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{9, 2, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing costume inventory update, got %d", msg.id)
	}
	var head, inUse int
	if err = db.DB.QueryRow("SELECT head FROM UserCostumeLoadout WHERE userID=1").Scan(&head); err != nil {
		t.Fatal(err)
	}
	if head != 471 {
		t.Fatalf("head costume %d", head)
	}
	if err = db.DB.QueryRow("SELECT inUse FROM UserInventory WHERE userID=1 AND slot=2").Scan(&inUse); err != nil {
		t.Fatal(err)
	}
	if inUse != 1 {
		t.Fatalf("first costume inUse %d", inUse)
	}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{9, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing replacement update, got %d", msg.id)
	}
	if err = db.DB.QueryRow("SELECT head FROM UserCostumeLoadout WHERE userID=1").Scan(&head); err != nil {
		t.Fatal(err)
	}
	if head != 475 {
		t.Fatalf("replacement head %d", head)
	}
	if err = db.DB.QueryRow("SELECT inUse FROM UserInventory WHERE userID=1 AND slot=2").Scan(&inUse); err != nil {
		t.Fatal(err)
	}
	if inUse != 0 {
		t.Fatalf("old costume still in use %d", inUse)
	}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{9, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing unequip update, got %d", msg.id)
	}
	if err = db.DB.QueryRow("SELECT head FROM UserCostumeLoadout WHERE userID=1").Scan(&head); err != nil {
		t.Fatal(err)
	}
	if head != 0 {
		t.Fatalf("head after unequip %d", head)
	}
}
