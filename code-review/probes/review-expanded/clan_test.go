package store

import "testing"

func TestAuditDormantClanDeposit(t *testing.T) {
	s := testStore(t)
	user := clanStorageUser(t, s, "auditmaster", 40000)
	if _, err := s.CreateClan(ClanCreate{
		MasterID:   user,
		Name:       "AuditClan",
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
	_, code, err := s.GiveClanItem(user, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if code != 1 {
		t.Fatalf("unexpired dormant 3-day item rejected by clan deposit, code=%d", code)
	}
}
