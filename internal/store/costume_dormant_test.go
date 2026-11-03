package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func costumeDB(t *testing.T) *Store {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "costume.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("dormantuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "DormantUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	return db
}

// TestEquipActivatesDormantCostume mirrors OnCostumeEquip: equipping a dormant
// copy sets status 1 and turns the stored relative duration into an absolute
// expiry date, otherwise readInventory drops it from the visible bag.
func TestEquipActivatesDormantCostume(t *testing.T) {
	db := costumeDB(t)
	if _, err := db.GrantReward(1, Grant{Items: []Reward{{ID: 471, Count: 1, Duration: 3, Dormant: true}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	changed, err := db.ToggleCostume(1, 2, "HeadCostume", 0)
	if err != nil {
		t.Fatal(err)
	}
	var status, expiry int
	if err = db.DB.QueryRow("SELECT status,expiryDate FROM UserInventory WHERE userID=1 AND slot=2").Scan(&status, &expiry); err != nil {
		t.Fatal(err)
	}
	now := int(time.Now().Unix() / 60)
	if status != 1 || expiry < now+3*1440-10 || expiry > now+3*1440+10 {
		t.Fatalf("status=%d expiry=%d, want status=1 and now+4320", status, expiry)
	}
	if len(changed) == 0 || changed[len(changed)-1].Status != 1 {
		t.Fatalf("returned item did not carry status 1: %+v", changed)
	}
	items, err := db.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, item := range items {
		if item.Slot == 2 {
			found = true
		}
	}
	if !found {
		t.Fatal("equipped costume disappeared from the visible inventory")
	}
}

// TestAutoEquipTimedCostume covers a freshly granted timed costume: the dormant
// copy still has a relative duration and must be auto-equipped and activated.
func TestAutoEquipTimedCostume(t *testing.T) {
	db := costumeDB(t)
	if _, err := db.GrantReward(1, Grant{Items: []Reward{{ID: 471, Count: 1, Duration: 3, Dormant: true}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	ok, err := db.AutoEquipCostume(1, 471, "HeadCostume", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("timed costume was not auto-equipped")
	}
	var status, expiry int
	if err = db.DB.QueryRow("SELECT status,expiryDate FROM UserInventory WHERE userID=1 AND slot=2").Scan(&status, &expiry); err != nil {
		t.Fatal(err)
	}
	now := int(time.Now().Unix() / 60)
	if status != 1 || expiry < now+3*1440-10 {
		t.Fatalf("auto-equipped status=%d expiry=%d", status, expiry)
	}
}

// TestZombieSkinZeroIsValid keeps the reference's 0..17 slot range.
func TestZombieSkinZeroIsValid(t *testing.T) {
	db := costumeDB(t)
	_, err := db.GrantReward(1, Grant{Items: []Reward{{ID: 8009, Count: 1, Dormant: true}}}, 100, 0)
	if err != nil {
		t.Fatal(err)
	}
	var slot int
	if err = db.DB.QueryRow("SELECT slot FROM UserInventory WHERE userID=1 AND itemID=8009").Scan(&slot); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ToggleCostume(1, slot, "ZombieSkinCostume", 0); err != nil {
		t.Fatalf("skin 0 rejected: %v", err)
	}
	var skin int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserZBCostumeLoadout WHERE userID=1 AND slot=0").Scan(&skin); err != nil {
		t.Fatal(err)
	}
	if skin != 1 {
		t.Fatal("skin 0 loadout row missing")
	}
	if _, err = db.ToggleCostume(1, slot, "ZombieSkinCostume", zbCostumeSlots); err != ErrItemUnavailable {
		t.Fatalf("out-of-range skin accepted: %v", err)
	}
}
