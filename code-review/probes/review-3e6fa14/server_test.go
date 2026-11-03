package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestReview3eGMRemovalRefreshesCache(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{{
		ID:      7001,
		Count:   1,
		Dormant: true,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.toggleNameplate(c, store.Item{
		ID:   7001,
		Slot: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.gmCommand(c, "/removeitem 7001 0", []string{"/removeitem", "7001", "0"}); err != nil {
		t.Fatal(err)
	}
	fresh, err := db.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.NameplateID != 0 {
		t.Fatal("database cleanup failed")
	}
	if c.character.NameplateID != 0 {
		t.Fatalf("GM removal keeps cache=%d while database=0", c.character.NameplateID)
	}
}
