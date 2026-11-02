package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestDisassembleRemovesSelectedItems(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "disassemble.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("disassembleuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "DisassembleUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	for slot, item := range map[int]int{2: 32, 3: 33, 4: 34} {
		if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,?,?,1,1,0,0,0,0,0,0,0,0,0,0,0)", slot, item); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{
		Store: db,
		Config: config.Config{
			InventorySlotMax: 10,
		},
	}
	c := &client{
		user: store.User{
			ID: 1,
		},
		character: store.Character{
			ID:   1,
			Name: "DisassembleUser",
		},
		version: true,
		queue:   make(chan outbound, 8),
	}
	// Remove slots 2 and 3, keep slot 4.
	request := []byte{7, 2, 0, 0, 2, 0, 0, 0, 0, 0, 0, 3, 0, 0, 0, 0, 0}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: request}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing inventory update, got %d", msg.id)
	}
	var rows int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND slot IN (2,3)").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("removed slots still present %d", rows)
	}
	var slot int
	if err = db.DB.QueryRow("SELECT slot FROM UserInventory WHERE userID=1 AND slot=4").Scan(&slot); err != nil {
		t.Fatal(err)
	}
	if slot != 4 {
		t.Fatalf("kept slot %d", slot)
	}

	// Removing an unknown slot is silent.
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{7, 1, 0, 0, 9, 0, 0, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if len(c.queue) != 0 {
		t.Fatalf("unknown slot produced %d packets", len(c.queue))
	}
}
