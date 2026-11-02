package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestOpenItemBoxConsumesKeysAndReturnsAwards(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "box.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("boxuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "BoxUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,3,8292,3,1,1,?,0,0,0,0,0,0,0,0,0)", time.Now().Unix()/60); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:     db,
		Config:    config.Config{InventorySlotMax: 10},
		items:     map[int]itemDefinition{8292: {ClassName: "LotteryKeyItem"}},
		itemBoxes: map[int]*itemBox{8292: {ID: 8292, Total: 1, Rates: []itemBoxRate{{Rate: 1, Grade: 1, Durations: []int{-1}, Items: []int{1234}}}}},
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{Name: "BoxUser"}, queue: make(chan outbound, 64)}
	request := []byte{4, 3, 0, 2, 0} // slot=3, count=2 (slot-first order)
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: request}); err != nil {
		t.Fatal(err)
	}
	if update := <-c.queue; update.id != 154 {
		t.Fatalf("expected inventory update, got packet %d", update.id)
	}
	msg := <-c.queue
	if msg.id != 78 {
		t.Fatalf("expected decoder result, got packet %d", msg.id)
	}
	r := &protocol.Reader{Data: msg.data}
	if r.U8() != 4 || r.U8() != 1 || r.U32() != 8292 || r.U16() != 0 || r.U32() != 0xFAD {
		t.Fatal("decoder result header mismatch")
	}
	if n := r.U8(); n != 2 {
		t.Fatalf("decoder result count %d", n)
	}
	for i := 0; i < 2; i++ {
		if r.U32() != 1234 || r.U16() != 0xFFFF || r.U16() != 1 || r.U8() != 1 || r.U8() != 0 || r.U16() != 0 || r.U16() != 0 {
			t.Fatal("decoder award mismatch")
		}
	}
	if r.Err != nil || r.Pos != len(r.Data) {
		t.Fatal("decoder result has an invalid tail")
	}
	var count int
	if err = db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND slot=3").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("decoder key count %d, want 1", count)
	}
}

func TestOpenItemBoxUnknownBoxDoesNotConsumeKey(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "box.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("noboxuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "NoBoxUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,8292,1,1,1,?,0,0,0,0,0,0,0,0,0)", time.Now().Unix()/60); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:     db,
		Config:    config.Config{InventorySlotMax: 10},
		items:     map[int]itemDefinition{8292: {ClassName: "LotteryKeyItem"}},
		itemBoxes: map[int]*itemBox{},
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{Name: "NoBoxUser"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{4, 1, 0, 2, 0}}); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	if msg.id != 78 || len(msg.data) < 2 || msg.data[0] != 4 || msg.data[1] != 0 {
		t.Fatal("expected decoder error reply")
	}
	var count int
	if err = db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND slot=2").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("unknown box consumed key, count %d", count)
	}
}

func TestOpenItemBoxCountFirstOrder(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "box.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("boxcount", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "BoxCount", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,3,8292,2,1,1,?,0,0,0,0,0,0,0,0,0)", time.Now().Unix()/60); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:     db,
		Config:    config.Config{InventorySlotMax: 10},
		items:     map[int]itemDefinition{8292: {ClassName: "LotteryKeyItem"}},
		itemBoxes: map[int]*itemBox{8292: {ID: 8292, Total: 1, Rates: []itemBoxRate{{Rate: 1, Grade: 1, Durations: []int{-1}, Items: []int{1234}}}}},
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{Name: "BoxCount"}, queue: make(chan outbound, 64)}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{4, 2, 0, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	if update := <-c.queue; update.id != 154 {
		t.Fatalf("expected inventory update, got packet %d", update.id)
	}
	msg := <-c.queue
	r := &protocol.Reader{Data: msg.data}
	if msg.id != 78 || r.U8() != 4 || r.U8() != 1 || r.U32() != 8292 {
		t.Fatal("count-first decoder result mismatch")
	}
	r.U16()
	r.U32()
	if n := r.U8(); n != 2 {
		t.Fatalf("decoder result count %d", n)
	}
	var rows int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=8292").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatal("exhausted decoder stack was not removed")
	}
	if err = db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=1234").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("decoder awards %d, want 2", rows)
	}
}
func TestOpenItemBoxFullInventoryKeepsKey(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "box.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("boxfull", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "BoxFull", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,8292,1,1,1,?,0,0,0,0,0,0,0,0,0)", time.Now().Unix()/60); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:     db,
		Config:    config.Config{InventorySlotMax: 1},
		items:     map[int]itemDefinition{8292: {ClassName: "LotteryKeyItem"}},
		itemBoxes: map[int]*itemBox{8292: {ID: 8292, Total: 1, Rates: []itemBoxRate{{Rate: 1, Grade: 1, Durations: []int{-1}, Items: []int{1234}}}}},
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{Name: "BoxFull"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{4, 2, 0, 1, 0}}); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	r := &protocol.Reader{Data: msg.data}
	if msg.id != 78 || r.U8() != 4 || r.U8() != 0 {
		t.Fatal("expected decoder inventory-full reply")
	}
	if text := r.CString(); text != "LOTTERY_FAIL_INVENTORY_FULL" {
		t.Fatalf("decoder error %q", text)
	}
	var count int
	if err = db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND itemID=8292").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("full inventory consumed key, count %d", count)
	}
}
