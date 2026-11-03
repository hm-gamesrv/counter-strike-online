package store

import "testing"

func TestReview33DormantCopyDoesNotKeepEquippedReference(t *testing.T) {
	s := costumeDB(t)
	if _, err := s.GrantReward(1, Grant{Items: []Reward{
		{
			ID:       471,
			Count:    1,
			Duration: 3,
			Dormant:  true,
		},
		{
			ID:       471,
			Count:    1,
			Duration: 7,
			Dormant:  true,
		},
	}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ToggleCostume(1, 2, "HeadCostume", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=1 AND slot=2", nowMinutes()-1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExpireItems(nowMinutes()); err != nil {
		t.Fatal(err)
	}
	var selected, equipped int
	if err := s.DB.QueryRow("SELECT head FROM UserCostumeLoadout WHERE userID=1").Scan(&selected); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=? AND inUse=1 AND status=1", selected).Scan(&equipped); err != nil {
		t.Fatal(err)
	}
	if selected != 0 && equipped == 0 {
		t.Fatalf("loadout still selects %d with no equipped copy; remaining copy is dormant", selected)
	}
}
