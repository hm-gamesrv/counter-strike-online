package store

import (
	"math"
	"testing"
)

func TestPurchaseRespectsNextSlotAndWireOffset(t *testing.T) {
	s := inventoryStore(t)
	if _, err := s.DB.Exec("DELETE FROM UserInventory WHERE userID=1 AND slot=2"); err != nil {
		t.Fatal(err)
	}
	reward := []Reward{{ID: 14, Count: 1}}
	if err := s.Purchase(1, true, 10, 0, reward, 2, 65533); err == nil {
		t.Fatal("allocated a slot that wraps after adding default slots")
	}
	ch, err := s.Character(1)
	if err != nil || ch.Points != 1000 {
		t.Fatal("failed allocation charged account", ch, err)
	}
	if err = s.Purchase(1, true, 10, 0, reward, 3, 65532); err != nil {
		t.Fatal(err)
	}
	items, err := s.Inventory(1)
	if err != nil || len(items) != 2 || items[1].Slot != 3 {
		t.Fatal("deleted highest slot was reused", items, err)
	}
}

func TestCurrencyOverflowRollsBack(t *testing.T) {
	s := inventoryStore(t)
	if err := s.SettleMatch("initialize-audit", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE UserCharacter SET points=? WHERE userID=1", int64(math.MaxInt64)); err != nil {
		t.Fatal(err)
	}
	if err := s.Purchase(1, true, 0, 1, []Reward{{ID: 14, Count: 1}}, 100, 0); err == nil {
		t.Fatal("purchase bonus overflow accepted")
	}
	if err := s.SettleMatch("overflow", []MatchReward{{UserID: 1, Points: 1}}); err == nil {
		t.Fatal("settlement overflow accepted")
	}
	ch, err := s.Character(1)
	if err != nil || int64(ch.Points) != math.MaxInt64 {
		t.Fatal("overflow corrupted balance", ch, err)
	}
	var count int
	if err = s.DB.QueryRow("SELECT count(*) FROM GoMatchSettlement WHERE matchID='overflow'").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed settlement left an idempotency record", count, err)
	}
}
