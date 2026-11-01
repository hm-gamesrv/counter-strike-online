package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestWeaponPartEquipAndUnequip(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "part.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("partuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "PartUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	now := int(time.Now().Unix() / 60)
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,4,1,1,1,?,0,0,0,0,0,0,0,0,0)", now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,3,4001,2,1,1,?,0,0,0,0,0,0,0,0,0)", now); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:  db,
		Config: config.Config{InventorySlotMax: 10},
		items:  map[int]itemDefinition{4: {Category: 1}, 4001: {Category: 13}},
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "PartUser"}, queue: make(chan outbound, 16)}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{21, 2, 2, 0, 3, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing part inventory update, got %d", msg.id)
	}
	msg := <-c.queue
	if msg.id != 78 || len(msg.data) < 4 || msg.data[0] != 21 || msg.data[1] != 0 {
		t.Fatalf("unexpected part check %v", msg.data)
	}
	var part1, part2, count int
	if err = db.DB.QueryRow("SELECT partSlot1,partSlot2 FROM UserInventory WHERE userID=1 AND slot=2").Scan(&part1, &part2); err != nil {
		t.Fatal(err)
	}
	if part1 != 4001 || part2 != 0 {
		t.Fatalf("weapon parts %d/%d", part1, part2)
	}
	if err = db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND slot=3").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("part count %d", count)
	}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{21, 1, 2, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg = <-c.queue; msg.id != 154 {
		t.Fatalf("missing unequip inventory update, got %d", msg.id)
	}
	if err = db.DB.QueryRow("SELECT partSlot1,partSlot2 FROM UserInventory WHERE userID=1 AND slot=2").Scan(&part1, &part2); err != nil {
		t.Fatal(err)
	}
	if part1 != 0 || part2 != 0 {
		t.Fatalf("parts after unequip %d/%d", part1, part2)
	}
}
