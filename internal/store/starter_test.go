package store

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func TestStarterInventoryAndOneBasedLoadoutConfiguration(t *testing.T) {
	s := testStore(t)
	if err := s.Register("starter", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	d := config.DefaultUser{Level: 1, PseudoDefaultItems: []int{12, 2, 161}, Loadouts: map[string]map[string]int{"0": {"1": 19, "2": 3, "3": 176, "4": 29}}}
	if err := s.CreateCharacter(1, "Starter", d); err != nil {
		t.Fatal(err)
	}
	var slots [4]int
	if err := s.DB.QueryRow("SELECT slot0,slot1,slot2,slot3 FROM UserLoadout WHERE userID=1 AND loadoutID=0").Scan(&slots[0], &slots[1], &slots[2], &slots[3]); err != nil {
		t.Fatal(err)
	}
	if slots != [4]int{19, 3, 176, 29} {
		t.Fatalf("configured loadout shifted: %v", slots)
	}
	items, err := s.Inventory(1)
	if err != nil || len(items) != 4 {
		t.Fatal(items, err)
	}
	for _, item := range items {
		if item.InUse != 1 || item.ID != 1001 && (item.Status != 1 || item.Lock != 2) || item.ID == 1001 && (item.Status != 0 || item.Lock != 0) {
			t.Fatalf("incorrect starter activation: %+v", item)
		}
	}
}
