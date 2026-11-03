package store

import "testing"

// TestDeletingNameplateClearsSelection: removing the inventory row must also
// drop the character's nameplate reference, otherwise a later login keeps
// advertising an item that no longer exists.
func TestDeletingNameplateClearsSelection(t *testing.T) {
	s := costumeDB(t)
	if _, err := s.GrantReward(1, Grant{Items: []Reward{{
		ID:      7001,
		Count:   1,
		Dormant: true,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ToggleNameplate(1, 7001, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteItems(1, []int{2}, nil); err != nil {
		t.Fatal(err)
	}
	character, err := s.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if character.NameplateID != 0 {
		t.Fatalf("deleted item is still selected: %d", character.NameplateID)
	}
}

// TestExpiredCostumeDoesNotBlockAutoEquip: an expired costume leaves a stale
// loadout entry; auto-equip must ignore it and clean it up.
func TestExpiredCostumeDoesNotBlockAutoEquip(t *testing.T) {
	s := costumeDB(t)
	if _, err := s.GrantReward(1, Grant{Items: []Reward{{
		ID:       471,
		Count:    1,
		Duration: 3,
		Dormant:  true,
	}}}, 100, 0); err != nil {
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
	if _, err := s.GrantReward(1, Grant{Items: []Reward{{
		ID:      472,
		Count:   1,
		Dormant: true,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	if equipped, err := s.AutoEquipCostume(1, 472, "HeadCostume", 0); err != nil {
		t.Fatal(err)
	} else if !equipped {
		t.Fatal("stale loadout entry blocked the new costume")
	}
}

// TestClanApprovalRespectsCapacity: queued applications do not reserve a slot,
// so approval must re-check the limit and keep the application when full.
func TestClanApprovalRespectsCapacity(t *testing.T) {
	s := testStore(t)
	master := clanStorageUser(t, s, "master", 40000)
	first := clanStorageUser(t, s, "firstuser", 0)
	second := clanStorageUser(t, s, "seconduser", 0)
	clan, err := s.CreateClan(ClanCreate{
		MasterID:       master,
		Name:           "CapacityClan",
		JoinMethod:     2,
		MaxMemberCount: 2,
	}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{first, second} {
		if code, _, err := s.JoinClan(id, clan, 1000); err != nil || code != -5 {
			t.Fatalf("queue application: code=%d error=%v", code, err)
		}
	}
	if code, _, err := s.ApproveClanJoin(master, "firstuser", 1000); err != nil || code != 1 {
		t.Fatalf("first approval: code=%d error=%v", code, err)
	}
	if code, _, err := s.ApproveClanJoin(master, "seconduser", 1000); err != nil || code != -6 {
		t.Fatalf("second approval should be refused: code=%d error=%v", code, err)
	}
	var members, capacity int
	if err := s.DB.QueryRow("SELECT memberCount,maxMemberCount FROM Clan WHERE clanID=?", clan).Scan(&members, &capacity); err != nil {
		t.Fatal(err)
	}
	if members > capacity {
		t.Fatalf("approval overflowed clan: members=%d capacity=%d", members, capacity)
	}
	var pending int
	if err := s.DB.QueryRow("SELECT count(*) FROM ClanMemberRequest WHERE clanID=? AND userID=?", clan, second).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 1 {
		t.Fatal("refused application was dropped instead of queued")
	}
}
