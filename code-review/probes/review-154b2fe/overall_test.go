package store

import "testing"

func TestReview154DeletingNameplateClearsSelection(t *testing.T) {
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
		t.Fatalf("deleted item is still selected after character reload: %d", character.NameplateID)
	}
}

func TestReview154ExpiredCostumeDoesNotBlockAutoEquip(t *testing.T) {
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
		t.Fatal("expired costume's stale loadout blocks the new costume")
	}
}

func TestReview154ClanApprovalsRespectCapacity(t *testing.T) {
	s := testStore(t)
	master := clanStorageUser(t, s, "master", 40000)
	a := clanStorageUser(t, s, "firstuser", 0)
	b := clanStorageUser(t, s, "seconduser", 0)
	clan, err := s.CreateClan(ClanCreate{
		MasterID:       master,
		Name:           "CapacityClan",
		JoinMethod:     2,
		MaxMemberCount: 2,
	}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{a, b} {
		if code, _, err := s.JoinClan(id, clan, 1000); err != nil || code != -5 {
			t.Fatalf("queue application: code=%d error=%v", code, err)
		}
	}
	for _, name := range []string{"firstuser", "seconduser"} {
		if _, _, err := s.ApproveClanJoin(master, name, 1000); err != nil {
			t.Fatal(err)
		}
	}
	var count, capacity int
	if err := s.DB.QueryRow("SELECT memberCount,maxMemberCount FROM Clan WHERE clanID=?", clan).Scan(&count, &capacity); err != nil {
		t.Fatal(err)
	}
	if count > capacity {
		t.Fatalf("approval overflowed clan: members=%d capacity=%d", count, capacity)
	}
}
