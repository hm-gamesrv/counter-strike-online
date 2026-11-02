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

func newEnhanceTestServer(t *testing.T) (*Server, *client, *store.Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "enhance.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("enhanceuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "EnhanceUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	now := int(time.Now().Unix() / 60)
	for slot, item := range map[int]int{2: 9000, 3: 9001} {
		count := 1
		if item == 9001 {
			count = 2
		}
		if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,?,?,?,1,0,?,0,0,0,0,0,0,0,0,0)", slot, item, count, now); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{
		Store:  db,
		Config: config.Config{InventorySlotMax: 10},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		items: map[int]itemDefinition{
			9000: {Category: 1, Grade: 1},
			9001: {Grade: 6},
			9002: {Category: 1, Grade: 1},
		},
		enhanceExp: map[int][]int{0: {0, 10, 20, 50, 100, 200, 500}, 1: {0, 20, 30, 70, 130, 250, 630}},
		enhanceMax: map[int]enhanceLimit{9000: {TotalMaxLv: 8, Caps: [7]int{3, 4, 5, 3, 2, 1, 30}}},
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "EnhanceUser"}, version: true, queue: make(chan outbound, 16)}
	return s, c, db
}

func TestEnhanceConsumesMaterialsAndLevelsUp(t *testing.T) {
	s, c, db := newEnhanceTestServer(t)
	request := []byte{14, 0, 0, 2, 0, 1, 0, 3, 0}
	if err := s.dispatch(c, protocol.Packet{ID: 78, Data: request}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing inventory update, got %d", msg.id)
	}
	result := <-c.queue
	if result.id != 78 {
		t.Fatalf("missing enhance result, got %d", result.id)
	}
	r := &protocol.Reader{Data: result.data}
	if r.U8() != 14 || r.U8() != 0 || r.U16() != 2 || r.U16() != 1 || r.U32() != 1 {
		t.Fatalf("unexpected success packet: %v at %d", r.Err, r.Pos)
	}
	if attribute := r.U8(); attribute > 6 {
		t.Fatalf("invalid enhanced attribute %d", attribute)
	}
	if r.U8() != 0 || r.Err != nil || r.Pos != len(r.Data) {
		t.Fatalf("unexpected success packet tail: %v at %d/%d", r.Err, r.Pos, len(r.Data))
	}
	var level, exp, value, count int
	if err := db.DB.QueryRow("SELECT enhancementLevel,enhancementExp,enhanceValue FROM UserInventory WHERE userID=1 AND slot=2").Scan(&level, &exp, &value); err != nil {
		t.Fatal(err)
	}
	if level != 1 || exp != 0 || value == 0 {
		t.Fatalf("enhanced target level=%d exp=%d value=%d", level, exp, value)
	}
	if err := db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND slot=3").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("material count %d", count)
	}

	// The second material is the last one and must remove the stack.
	if err := s.dispatch(c, protocol.Packet{ID: 78, Data: request}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing second inventory update, got %d", msg.id)
	}
	if result := <-c.queue; result.id != 78 {
		t.Fatalf("missing second enhance result, got %d", result.id)
	}
	var rows int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND slot=3").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("consumed material row still present %d", rows)
	}
	if err := db.DB.QueryRow("SELECT enhancementLevel FROM UserInventory WHERE userID=1 AND slot=2").Scan(&level); err != nil {
		t.Fatal(err)
	}
	if level != 2 {
		t.Fatalf("target level after second enhance %d", level)
	}
}

func TestEnhanceRejectsUnsupportedTargets(t *testing.T) {
	s, c, db := newEnhanceTestServer(t)
	// Item 9002 has no ReinforceMaxLv row.
	if _, err := db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,4,9002,1,1,0,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{14, 0, 0, 4, 0, 1, 0, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 78 || len(msg.data) != 2 || msg.data[0] != 14 || msg.data[1] != 5 {
		t.Fatalf("unsupported target reply %d %v", msg.id, msg.data)
	}

	// A target already at its maximum level is rejected before consumption.
	if _, err := db.DB.Exec("UPDATE UserInventory SET enhancementLevel=8 WHERE userID=1 AND slot=2"); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{14, 0, 0, 2, 0, 1, 0, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 78 || len(msg.data) != 2 || msg.data[1] != 5 {
		t.Fatalf("max-level target reply %d %v", msg.id, msg.data)
	}
	var count int
	if err := db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND slot=3").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("rejected enhance consumed materials: %d", count)
	}
}
func TestEnhanceDecreaseWithAntiEnhancers(t *testing.T) {
	s, c, db := newEnhanceTestServer(t)
	if _, err := db.DB.Exec("UPDATE UserInventory SET enhancementLevel=2,enhancementExp=5,enhanceValue=? WHERE userID=1 AND slot=2", 0x11); err != nil {
		t.Fatal(err)
	}
	for slot, item := range map[int]int{5: 5206, 6: 5205} {
		if _, err := db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,?,?,1,1,0,0,0,0,0,0,0,0,0,0,0)", slot, item); err != nil {
			t.Fatal(err)
		}
	}

	// Anti-damage clears only the damage attribute.
	if err := s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{14, 2, 0, 2, 0, 0, 5, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing decrease inventory update, got %d", msg.id)
	}
	result := <-c.queue
	r := &protocol.Reader{Data: result.data}
	if result.id != 78 || r.U8() != 14 || r.U8() != 3 || r.U16() != 5 || r.U16() != 1 || r.U32() != 1 || r.U8() != 0 {
		t.Fatalf("unexpected decrease packet %d %v", result.id, result.data)
	}
	var level, exp, value int
	if err := db.DB.QueryRow("SELECT enhancementLevel,enhancementExp,enhanceValue FROM UserInventory WHERE userID=1 AND slot=2").Scan(&level, &exp, &value); err != nil {
		t.Fatal(err)
	}
	if level != 1 || exp != 5 || value != 0x10 {
		t.Fatalf("decreased target level=%d exp=%d value=%#x", level, exp, value)
	}
	if rows := inventoryRows(t, db, 5); rows != 0 {
		t.Fatalf("anti-damage item not consumed: %d", rows)
	}

	// Full reset clears the remaining accuracy attribute.
	if err := s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{14, 2, 0, 2, 0, 0, 6, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing reset inventory update, got %d", msg.id)
	}
	result = <-c.queue
	r = &protocol.Reader{Data: result.data}
	if result.id != 78 || r.U8() != 14 || r.U8() != 4 || r.U16() != 6 || r.U16() != 0 || r.U32() != 0 || r.U8() != 0xFF {
		t.Fatalf("unexpected reset packet %d %v", result.id, result.data)
	}
	if err := db.DB.QueryRow("SELECT enhancementLevel,enhanceValue FROM UserInventory WHERE userID=1 AND slot=2").Scan(&level, &value); err != nil {
		t.Fatal(err)
	}
	if level != 0 || value != 0 {
		t.Fatalf("reset target level=%d value=%#x", level, value)
	}
	if rows := inventoryRows(t, db, 6); rows != 0 {
		t.Fatalf("full reset item not consumed: %d", rows)
	}
}

func TestEnhanceDecreaseRejectsEmptyAttribute(t *testing.T) {
	s, c, db := newEnhanceTestServer(t)
	if _, err := db.DB.Exec("UPDATE UserInventory SET enhancementLevel=1,enhanceValue=? WHERE userID=1 AND slot=2", 0x10); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,5,5206,1,1,0,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	if err := s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{14, 2, 0, 2, 0, 0, 5, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 78 || len(msg.data) != 2 || msg.data[1] != 5 {
		t.Fatalf("unexpected rejection %d %v", msg.id, msg.data)
	}
	var rows int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND slot=5").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("rejected decrease consumed item: %d", rows)
	}
}
func inventoryRows(t *testing.T, db *store.Store, slot int) int {
	t.Helper()
	var rows int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND slot=?", slot).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}
