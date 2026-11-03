package store

import "testing"

func TestReview4308ClanDepositReturnsPromotedCopy(t *testing.T) {
	s := testStore(t)
	id := clanStorageUser(t, s, "reviewmaster", 40000)
	if _, err := s.CreateClan(ClanCreate{
		MasterID: id,
		Name:     "ReviewClan",
	}, 1000); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.GrantReward(id, Grant{Items: []Reward{{
			ID:        12,
			Count:     1,
			Exclusive: true,
		}}}, 100, 0); err != nil {
			t.Fatal(err)
		}
	}
	changed, code, err := s.GiveClanItem(id, 0, 3)
	if err != nil || code != 1 {
		t.Fatalf("deposit code=%d err=%v", code, err)
	}
	var selected int
	if err := s.DB.QueryRow("SELECT status FROM UserInventory WHERE userID=? AND slot=2", id).Scan(&selected); err != nil || selected != 1 {
		t.Fatalf("database promotion=%d err=%v", selected, err)
	}
	for _, item := range changed {
		if item.Slot == 2 && item.ID == 12 && item.Status == 1 {
			return
		}
	}
	t.Fatalf("promoted slot 2 missing from client patch: %+v", changed)
}
