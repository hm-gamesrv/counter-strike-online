package store

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func TestAutoEquipCostume(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "costume.db3"))
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
	for slot, item := range map[int]int{2: 471, 3: 475, 4: 500} {
		if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,?,?,1,0,0,0,0,0,0,0,0,0,0,0,0)", slot, item); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.AutoEquipCostume(1, 471, "HeadCostume", 0); err != nil {
		t.Fatal(err)
	}
	var head, inUse, status int
	if err = db.DB.QueryRow("SELECT head FROM UserCostumeLoadout WHERE userID=1").Scan(&head); err != nil {
		t.Fatal(err)
	}
	if head != 471 {
		t.Fatalf("auto-equipped head %d", head)
	}
	if err = db.DB.QueryRow("SELECT inUse,status FROM UserInventory WHERE userID=1 AND slot=2").Scan(&inUse, &status); err != nil {
		t.Fatal(err)
	}
	if inUse != 1 || status != 1 {
		t.Fatalf("auto-equipped item inUse=%d status=%d", inUse, status)
	}

	// An occupied slot must not be replaced by a newly granted costume.
	if _, err = db.AutoEquipCostume(1, 475, "HeadCostume", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.DB.QueryRow("SELECT head FROM UserCostumeLoadout WHERE userID=1").Scan(&head); err != nil {
		t.Fatal(err)
	}
	if head != 471 {
		t.Fatalf("occupied head slot changed to %d", head)
	}
	if err = db.DB.QueryRow("SELECT inUse FROM UserInventory WHERE userID=1 AND slot=3").Scan(&inUse); err != nil {
		t.Fatal(err)
	}
	if inUse != 0 {
		t.Fatalf("dormant costume equipped: %d", inUse)
	}

	// Zombie skins use their own table and skin-type slot.
	if _, err = db.AutoEquipCostume(1, 500, "ZombieSkinCostume", 2); err != nil {
		t.Fatal(err)
	}
	var skin int
	if err = db.DB.QueryRow("SELECT itemID FROM UserZBCostumeLoadout WHERE userID=1 AND slot=2").Scan(&skin); err != nil {
		t.Fatal(err)
	}
	if skin != 500 {
		t.Fatalf("auto-equipped zombie skin %d", skin)
	}

	// Missing inventory rows are ignored.
	if _, err = db.AutoEquipCostume(1, 999, "HeadCostume", 0); err != nil {
		t.Fatal(err)
	}
}
