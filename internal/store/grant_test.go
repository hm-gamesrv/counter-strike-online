package store

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func TestPurchaseActivationAndSelectedCopyRollback(t *testing.T) {
	s := testStore(t)
	if err := s.Register("grant", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(1, "Grant", config.DefaultUser{Level: 1, Points: 1000, PseudoDefaultItems: []int{12}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("CREATE TRIGGER fail_grant BEFORE INSERT ON UserInventory WHEN NEW.itemID=14 BEGIN SELECT RAISE(ABORT,'test grant'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.Purchase(1, true, 100, 0, []Reward{{ID: 12, Count: 1, Exclusive: true}, {ID: 14, Count: 1}}, 100, 0); err == nil {
		t.Fatal("failed bundle committed")
	}
	items, err := s.Inventory(1)
	ch, chErr := s.Character(1)
	if err != nil || chErr != nil || ch.Points != 1000 || len(items) != 2 || items[0].Status != 1 || items[0].InUse != 1 {
		t.Fatal("failed bundle changed the selected weapon or balance", items, ch, err, chErr)
	}
	if _, err = s.DB.Exec("DROP TRIGGER fail_grant"); err != nil {
		t.Fatal(err)
	}
	grants := []Reward{{ID: 12, Count: 1, Exclusive: true}, {ID: 1001, Count: 2, Unselected: true}, {ID: 14, Count: 1, Dormant: true}, {ID: 16, Count: 1, Dormant: true}}
	if err = s.Purchase(1, true, 100, 0, grants, 100, 0); err != nil {
		t.Fatal(err)
	}
	items, err = s.Inventory(1)
	if err != nil || len(items) != 6 {
		t.Fatal(items, err)
	}
	if items[0].Status != 0 || items[2].ID != 12 || items[2].Status != 1 || items[2].InUse != 1 || items[3].Status != 0 || items[3].InUse != 1 {
		t.Fatal("weapon replacement or countable activation incorrect", items)
	}
	for _, item := range items[4:] {
		if item.InUse != 0 || item.Status != 0 {
			t.Fatal("dormant item activated", item)
		}
	}
}
