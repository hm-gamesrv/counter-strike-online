package store

import "testing"

// TestClanDepositAcceptsDormantItem: a dormant copy keeps a relative day count,
// and every inventory reader must treat it as live instead of expired.
func TestClanDepositAcceptsDormantItem(t *testing.T) {
	s := testStore(t)
	user := clanStorageUser(t, s, "dormantmaster", 40000)
	if _, err := s.CreateClan(ClanCreate{
		MasterID:   user,
		Name:       "DormantClan",
		JoinMethod: 3,
	}, 1000); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantReward(user, Grant{
		Items: []Reward{
			{
				ID:       270,
				Count:    1,
				Duration: 3,
				Dormant:  true,
			},
		},
	}, 100, 0); err != nil {
		t.Fatal(err)
	}
	if _, code, err := s.GiveClanItem(user, 0, 2); err != nil {
		t.Fatal(err)
	} else if code != 1 {
		t.Fatalf("dormant 3-day item rejected by clan deposit, code=%d", code)
	}
}
