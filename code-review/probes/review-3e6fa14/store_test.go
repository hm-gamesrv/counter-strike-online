package store

import "testing"

func TestReview3eExpiryPromotesRemainingWeapon(t *testing.T) {
	s := costumeDB(t)
	for _, days := range []int{7, 1} {
		if _, err := s.GrantReward(1, Grant{Items: []Reward{{
			ID:        12,
			Count:     1,
			Duration:  days,
			Exclusive: true,
		}}}, 100, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.DB.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=1 AND slot=3", nowMinutes()-1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExpireItems(nowMinutes()); err != nil {
		t.Fatal(err)
	}
	items, err := s.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == 12 && item.InUse == 1 && item.Status == 1 {
			return
		}
	}
	t.Fatal("expiry leaves a live 7-day weapon unselected after removing the selected copy")
}
