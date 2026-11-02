package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestToggleEffectItemSwitchesInventoryAndExtended(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "effect.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("effectuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "EffectUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	for _, row := range [][2]int{{5, 7001}, {6, 7002}} {
		if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,?,?,1,0,0,0,0,0,0,0,0,0,0,0,0)", row[0], row[1]); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := db.ToggleEffectItem(1, 7001, 5, "zbRespawnEffect")
	if err != nil || len(changed) == 0 {
		t.Fatalf("activate %v %v", changed, err)
	}
	var effect, status, inUse int
	if err = db.DB.QueryRow("SELECT zbRespawnEffect FROM UserCharacterExtended WHERE userID=1").Scan(&effect); err != nil || effect != 7001 {
		t.Fatalf("extended %d %v", effect, err)
	}
	if err = db.DB.QueryRow("SELECT status,inUse FROM UserInventory WHERE userID=1 AND slot=5").Scan(&status, &inUse); err != nil || status != 1 || inUse != 1 {
		t.Fatalf("inventory %d %d %v", status, inUse, err)
	}

	// Switching to another effect clears the previous item's in-use flag.
	if _, err = db.ToggleEffectItem(1, 7002, 6, "zbRespawnEffect"); err != nil {
		t.Fatal(err)
	}
	if err = db.DB.QueryRow("SELECT inUse FROM UserInventory WHERE userID=1 AND slot=5").Scan(&inUse); err != nil || inUse != 0 {
		t.Fatalf("previous effect still in use: %d %v", inUse, err)
	}
	if err = db.DB.QueryRow("SELECT zbRespawnEffect FROM UserCharacterExtended WHERE userID=1").Scan(&effect); err != nil || effect != 7002 {
		t.Fatalf("extended %d %v", effect, err)
	}

	// Toggling the active item turns the effect off.
	if _, err = db.ToggleEffectItem(1, 7002, 6, "zbRespawnEffect"); err != nil {
		t.Fatal(err)
	}
	if err = db.DB.QueryRow("SELECT zbRespawnEffect FROM UserCharacterExtended WHERE userID=1").Scan(&effect); err != nil || effect != 0 {
		t.Fatalf("effect not cleared: %d %v", effect, err)
	}
	if err = db.DB.QueryRow("SELECT inUse FROM UserInventory WHERE userID=1 AND slot=6").Scan(&inUse); err != nil || inUse != 0 {
		t.Fatalf("inventory not cleared: %d %v", inUse, err)
	}
	if _, err = db.ToggleEffectItem(1, 7002, 99, "zbRespawnEffect"); err == nil {
		t.Fatal("missing slot accepted")
	}
	if _, err = db.ToggleEffectItem(1, 7002, 6, "nameplateID"); err == nil {
		t.Fatal("arbitrary column accepted")
	}
}

func TestItemRequestTogglesCombatInfoItem(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "killer.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("killeruser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "KillerUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,7,7003,1,0,0,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	c := &client{user: store.User{ID: 1}, queue: make(chan outbound, 8)}
	s := &Server{
		Store: db,
		items: map[int]itemDefinition{7003: {ClassName: "CombatInfoItem"}},
	}
	p := &protocol.Writer{}
	p.U8(0)
	p.U8(0)
	p.U16(7)
	p.U8(0)
	p.U16(1)
	if err = s.itemRequest(c, &protocol.Reader{Data: p.Data}); err != nil {
		t.Fatal(err)
	}
	var effect int
	if err = db.DB.QueryRow("SELECT killerMarkEffect FROM UserCharacterExtended WHERE userID=1").Scan(&effect); err != nil || effect != 7003 {
		t.Fatalf("killer mark %d %v", effect, err)
	}
	message := <-c.queue
	if message.id != 154 {
		t.Fatalf("inventory update packet %d", message.id)
	}
}

func TestToggleNameplateAndUserInfo(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "plate.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("plateuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "PlateUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,9,8001,1,0,0,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	changed, nameplate, err := db.ToggleNameplate(1, 8001, 9)
	if err != nil || nameplate != 8001 || len(changed) == 0 {
		t.Fatalf("activate %d %v %v", nameplate, changed, err)
	}
	character, err := db.Character(1)
	if err != nil || character.NameplateID != 8001 {
		t.Fatalf("character nameplate %d %v", character.NameplateID, err)
	}
	if _, nameplate, err = db.ToggleNameplate(1, 8001, 9); err != nil || nameplate != 0 {
		t.Fatalf("deactivate %d %v", nameplate, err)
	}

	character.NameplateID = 8001
	w := &protocol.Writer{}
	userInfo(w, character)
	r := &protocol.Reader{Data: w.Data}
	if flags := r.U32(); flags&1 == 0 {
		t.Fatalf("nameplate flag missing: %x", flags)
	}
	if r.U32() != 1 || r.U8() != 0 || r.U32() != 8001 || r.CString() != character.Name {
		t.Fatalf("nameplate block malformed: %v", w.Data[:24])
	}
}

func TestItemRequestTogglesLobbyBackground(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "bg.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("bguser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "BgUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,11,8002,1,0,0,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	c := &client{user: store.User{ID: 1}, queue: make(chan outbound, 8)}
	s := &Server{
		Store: db,
		items: map[int]itemDefinition{8002: {ClassName: "LobbyBG"}},
	}
	p := &protocol.Writer{}
	p.U8(0)
	p.U8(0)
	p.U16(11)
	p.U8(0)
	p.U16(1)
	if err = s.itemRequest(c, &protocol.Reader{Data: p.Data}); err != nil {
		t.Fatal(err)
	}
	character, err := db.Character(1)
	if err != nil || character.NameplateID != 8002 {
		t.Fatalf("nameplate %d %v", character.NameplateID, err)
	}
	if c.character.NameplateID != 8002 {
		t.Fatal("client character not refreshed")
	}
}
