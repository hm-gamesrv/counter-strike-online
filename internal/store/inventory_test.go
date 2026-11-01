package store

import (
	"errors"
	"sync"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func inventoryStore(t *testing.T) *Store {
	t.Helper()
	s := testStore(t)
	if err := s.Register("items", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(1, "Items", config.DefaultUser{Level: 1, Points: 1000, PseudoDefaultItems: []int{12}}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestItemActivationSelectionProtectionAndRollback(t *testing.T) {
	s := inventoryStore(t)
	if err := s.Purchase(1, true, 0, 0, []Reward{{ID: 12, Count: 1, Duration: 3, Dormant: true}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	items, _ := s.Inventory(1)
	if len(items) != 3 || items[2].Expiry != 3 {
		t.Fatal("relative dormant duration hidden or converted too early", items)
	}
	before := nowMinutes()
	changed, err := s.ChangeItem(1, 3, ActivateItem, nil)
	if err != nil || len(changed) != 1 || changed[0].InUse != 1 || changed[0].Status != 0 || changed[0].Expiry < before+4320 || changed[0].Expiry > nowMinutes()+4320 {
		t.Fatal("activation did not preserve selected copy or convert days", changed, err)
	}
	expiry := changed[0].Expiry
	changed, err = s.ChangeItem(1, 3, ActivateItem, nil)
	if err != nil || changed[0].Expiry != expiry {
		t.Fatal("repeated activation extended lifetime", changed, err)
	}
	if _, err = s.ChangeItem(2, 3, SelectItem, nil); !errors.Is(err, ErrItemUnavailable) {
		t.Fatal("other user's slot accessible", err)
	}
	if _, err = s.ChangeItem(1, 1, ToggleItemLock, nil); !errors.Is(err, ErrItemProtected) {
		t.Fatal("special lock changed", err)
	}
	if _, err = s.ChangeItem(1, 3, ToggleItemLock, []int{12}); !errors.Is(err, ErrItemProtected) {
		t.Fatal("default ID protection ignored", err)
	}
	if _, err = s.DB.Exec("CREATE TRIGGER fail_select BEFORE UPDATE ON UserInventory WHEN NEW.slot=3 BEGIN SELECT RAISE(ABORT,'test selection'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangeItem(1, 3, SelectItem, nil); err == nil {
		t.Fatal("failed selection committed")
	}
	items, _ = s.Inventory(1)
	if items[0].Status != 1 || items[2].Status != 0 {
		t.Fatal("failed selection deselected old copy", items)
	}
	if _, err = s.DB.Exec("DROP TRIGGER fail_select"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, slot := range []int{1, 3, 1, 3} {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			if _, err := s.ChangeItem(1, slot, SelectItem, nil); err != nil {
				t.Error(err)
			}
		}(slot)
	}
	wg.Wait()
	items, _ = s.Inventory(1)
	if items[0].Status+items[2].Status != 1 {
		t.Fatal("concurrent selection selected multiple copies", items)
	}
	if _, err = s.DB.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=1 AND slot=3", nowMinutes()-1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangeItem(1, 3, SelectItem, nil); !errors.Is(err, ErrItemUnavailable) {
		t.Fatal("expired weapon selected", err)
	}
}

func TestStackingInFullInventoryAndOverflowRollback(t *testing.T) {
	s := inventoryStore(t)
	grant := Reward{ID: 1001, Count: 4, Unselected: true, Stackable: true}
	if err := s.Purchase(1, true, 10, 0, []Reward{grant}, 2, 0); err != nil {
		t.Fatal("existing stack should fit full inventory", err)
	}
	items, _ := s.Inventory(1)
	if len(items) != 2 || items[1].Count != 5 {
		t.Fatal("stack inserted another slot", items)
	}
	grant.Count = 65535
	if err := s.Purchase(1, true, 10, 0, []Reward{grant}, 2, 0); err == nil {
		t.Fatal("wire count overflow accepted")
	}
	ch, _ := s.Character(1)
	items, _ = s.Inventory(1)
	if ch.Points != 990 || items[1].Count != 5 {
		t.Fatal("overflow charged or changed inventory", ch, items)
	}
}

func TestPasswordBoxCounterDoesNotUseInventorySlots(t *testing.T) {
	s := inventoryStore(t)
	if err := s.Purchase(1, true, 10, 0, []Reward{{ID: 166, Count: 3}}, 2, 0); err != nil {
		t.Fatal(err)
	}
	ch, _ := s.Character(1)
	items, _ := s.Inventory(1)
	if ch.PasswordBoxes != 3 || len(items) != 2 {
		t.Fatal("password box stored as inventory item", ch, items)
	}
	if err := s.Purchase(1, true, 10, 0, []Reward{{ID: 166, Count: 65535}}, 2, 0); err == nil {
		t.Fatal("counter overflow accepted")
	}
	ch, _ = s.Character(1)
	if ch.PasswordBoxes != 3 || ch.Points != 990 {
		t.Fatal("counter overflow charged account", ch)
	}
}
