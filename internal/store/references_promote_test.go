package store

import "testing"

// TestDeletingSelectedWeaponPromotesCopy: dismantling the selected copy must hand
// the selection to a remaining live copy, otherwise hostInventory drops it.
func TestDeletingSelectedWeaponPromotesCopy(t *testing.T) {
	s := costumeDB(t)
	for range 2 {
		if _, err := s.GrantReward(1, Grant{Items: []Reward{{
			ID:        12,
			Count:     1,
			Exclusive: true,
		}}}, 100, 0); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	selected := 0
	for _, item := range items {
		if item.ID == 12 && item.Status == 1 {
			selected = item.Slot
		}
	}
	if selected == 0 {
		t.Fatal("no selected weapon copy in the fixture")
	}
	if _, err := s.DeleteItems(1, []int{selected}, nil); err != nil {
		t.Fatal(err)
	}
	items, err = s.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == 12 && item.Status == 1 && item.InUse == 1 {
			return
		}
	}
	t.Fatal("remaining weapon copy stayed unselected after the selected one was removed")
}

// TestDormantCopyDoesNotKeepLoadout: a dormant leftover must not keep a costume
// slot occupied after the equipped copy expires.
func TestDormantCopyDoesNotKeepLoadout(t *testing.T) {
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
		t.Fatalf("loadout still selects %d with no equipped copy", selected)
	}
}
