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
func TestCostumeActivateFromCharacterTab(t *testing.T) {
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
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "CostumeUser"}, queue: make(chan outbound, 16)}
	// kind 0 (use), inventoryType 8 (character tab), slot, padding byte, count.
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{0, 8, 2, 0, 0, 1, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing costume inventory update, got %d", msg.id)
	}
	var head int
	if err = db.DB.QueryRow("SELECT head FROM UserCostumeLoadout WHERE userID=1").Scan(&head); err != nil {
		t.Fatal(err)
	}
	if head != 471 {
		t.Fatalf("head costume %d", head)
	}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{0, 8, 3, 0, 0, 1, 0}}); err != nil {
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
}

func TestCostumeDoesNotDuplicateLoadout(t *testing.T) {
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
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,471,1,1,0,?,0,0,0,0,0,0,0,0,0)", now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserCostumeLoadout(userID,head,back,arm,pelvis,face,tattoo,pet) VALUES(1,471,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{InventorySlotMax: 10}, items: map[int]itemDefinition{471: {ClassName: "HeadCostume"}}}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "CostumeUser"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{9, 2, 0}}); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserCostumeLoadout WHERE userID=1").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("costume loadout rows %d", rows)
	}
}
func TestZombieSkinCostumeToggle(t *testing.T) {
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
	for slot, item := range map[int]int{2: 500, 3: 501, 4: 502} {
		if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,?,?,1,1,0,?,0,0,0,0,0,0,0,0,0)", slot, item, now); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{
		Store:  db,
		Config: config.Config{InventorySlotMax: 10},
		items: map[int]itemDefinition{
			500: {ClassName: "ZombieSkinCostume", ZombieSkin: 2},
			501: {ClassName: "ZombieSkinCostume", ZombieSkin: 2},
			502: {ClassName: "ZombieSkinCostume"},
		},
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "CostumeUser"}, queue: make(chan outbound, 16)}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{15, 2, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing zombie skin update, got %d", msg.id)
	}
	var skin int
	if err = db.DB.QueryRow("SELECT itemID FROM UserZBCostumeLoadout WHERE userID=1 AND slot=2").Scan(&skin); err != nil {
		t.Fatal(err)
	}
	if skin != 500 {
		t.Fatalf("equipped zombie skin %d", skin)
	}
	var rows int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserCostumeLoadout WHERE userID=1").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("zombie skin wrote regular costume loadout %d", rows)
	}

	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{15, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing zombie skin replacement update, got %d", msg.id)
	}
	if err = db.DB.QueryRow("SELECT itemID FROM UserZBCostumeLoadout WHERE userID=1 AND slot=2").Scan(&skin); err != nil {
		t.Fatal(err)
	}
	if skin != 501 {
		t.Fatalf("replaced zombie skin %d", skin)
	}
	var inUse int
	if err = db.DB.QueryRow("SELECT inUse FROM UserInventory WHERE userID=1 AND slot=2").Scan(&inUse); err != nil {
		t.Fatal(err)
	}
	if inUse != 0 {
		t.Fatalf("old zombie skin still in use %d", inUse)
	}

	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{15, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing zombie skin unequip update, got %d", msg.id)
	}
	if err = db.DB.QueryRow("SELECT count(*) FROM UserZBCostumeLoadout WHERE userID=1 AND slot=2").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("zombie skin row still present %d", rows)
	}

	// ZombieSkin 0 is a real slot (the reference allows 0..17), so an item
	// without the column equips into slot 0 instead of being rejected.
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{15, 4, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing zombie skin update for slot 0, got %d", msg.id)
	}
	if err = db.DB.QueryRow("SELECT itemID FROM UserZBCostumeLoadout WHERE userID=1 AND slot=0").Scan(&skin); err != nil {
		t.Fatal(err)
	}
	if skin != 502 {
		t.Fatalf("equipped slot 0 skin %d", skin)
	}
	// Slot 18 is outside ZB_COSTUME_SLOT_COUNT_MAX and must be rejected.
	s.items[503] = itemDefinition{ClassName: "ZombieSkinCostume", ZombieSkin: 18}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,5,503,1,1,0,?,0,0,0,0,0,0,0,0,0)", now); err != nil {
		t.Fatal(err)
	}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: []byte{15, 5, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 67 {
		t.Fatalf("missing rejection notice for skin 18, got %d", msg.id)
	}
}
func TestAutoEquipCostumesHelper(t *testing.T) {
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
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,471,1,0,0,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, items: map[int]itemDefinition{471: {ClassName: "HeadCostume"}}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	equipped := s.autoEquipCostumes(1, []store.Reward{{ID: 471}})
	if !equipped[471] {
		t.Fatal("costume was not reported as equipped")
	}
	var head int
	if err = db.DB.QueryRow("SELECT head FROM UserCostumeLoadout WHERE userID=1").Scan(&head); err != nil {
		t.Fatal(err)
	}
	if head != 471 {
		t.Fatalf("auto-equipped head %d", head)
	}
}
