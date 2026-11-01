package store

import (
	"errors"
	"sync"
	"testing"
)

func TestConsumableRewardAtomicityAndConcurrentUse(t *testing.T) {
	s := inventoryStore(t) // Two occupied slots, including the starter gift at slot 2.
	g := Grant{SourceSlot: 2, SourceItem: 1001, Consume: 1, MinimumLevel: 1, Points: 100, Exp: 250, Items: []Reward{{ID: 14, Count: 1}, {ID: 15, Count: 1}}}
	if _, err := s.GrantReward(1, g, 2, 0); err == nil {
		t.Fatal("full bundle did not reject capacity overflow")
	}
	ch, _ := s.Character(1)
	items, _ := s.Inventory(1)
	if ch.Points != 1000 || ch.Exp != 0 || len(items) != 2 || items[1].ID != 1001 {
		t.Fatal("full inventory consumed source or granted partial reward", ch, items)
	}
	if _, err := s.DB.Exec("CREATE TRIGGER fail_reward BEFORE INSERT ON UserInventory WHEN NEW.itemID=15 BEGIN SELECT RAISE(ABORT,'test reward'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantReward(1, g, 100, 0); err == nil {
		t.Fatal("injected failure committed reward")
	}
	items, _ = s.Inventory(1)
	ch, _ = s.Character(1)
	if len(items) != 2 || items[1].ID != 1001 || ch.Points != 1000 {
		t.Fatal("insert failure lost source", items, ch)
	}
	if _, err := s.DB.Exec("DROP TRIGGER fail_reward"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.GrantReward(1, g, 100, 0); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	ch, err := s.Character(1)
	items, invErr := s.Inventory(1)
	if err != nil || invErr != nil || success != 1 || ch.Points != 1100 || ch.Exp != 250 || ch.Level != 3 || len(items) != 3 {
		t.Fatal("reward duplicated or lost", success, ch, items, err, invErr)
	}
	for _, item := range items {
		if item.ID == 1001 {
			t.Fatal("empty source still present")
		}
	}
}

func TestConsumableRewardRestrictionsAndFreeGrant(t *testing.T) {
	s := inventoryStore(t)
	g := Grant{SourceSlot: 2, SourceItem: 1001, Consume: 1, MinimumLevel: 4, Items: []Reward{{ID: 14, Count: 1}}}
	if _, err := s.GrantReward(1, g, 100, 0); !errors.Is(err, ErrRewardLevel) {
		t.Fatal(err)
	}
	g.MinimumLevel = 1
	if _, err := s.GrantReward(2, g, 100, 0); err == nil {
		t.Fatal("other account source accepted")
	}
	g.Protected = []int{1001}
	if _, err := s.GrantReward(1, g, 100, 0); !errors.Is(err, ErrItemProtected) {
		t.Fatal(err)
	}
	g.Protected = nil
	if _, err := s.DB.Exec("UPDATE UserInventory SET lockStatus=1 WHERE userID=1 AND slot=2"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantReward(1, g, 100, 0); !errors.Is(err, ErrItemProtected) {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE UserInventory SET lockStatus=0,expiryDate=? WHERE userID=1 AND slot=2", nowMinutes()-1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantReward(1, g, 100, 0); !errors.Is(err, ErrItemUnavailable) {
		t.Fatal(err)
	}
	if _, err := s.GrantReward(1, Grant{Points: 10, HonorPoints: 2}, 100, 0); err != nil {
		t.Fatal(err)
	}
	ch, err := s.Character(1)
	if err != nil || ch.Points != 1010 || ch.HonorPoints != 2 {
		t.Fatal(ch, err)
	}
}
