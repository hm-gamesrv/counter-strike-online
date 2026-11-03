package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestReview33RemovalRefreshesCharacterCache(t *testing.T) {
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
	if err := s.itemRequest(c, &protocol.Reader{Data: []byte{7, 1, 0, 0, 2, 0, 0, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	fresh, err := db.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.NameplateID != 0 {
		t.Fatal("database cleanup failed")
	}
	if c.character.NameplateID != fresh.NameplateID {
		t.Fatalf("cache still advertises deleted background %d, database=%d", c.character.NameplateID, fresh.NameplateID)
	}
}

func TestReview33DeletingSelectedWeaponPromotesRemainingCopy(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	for range 2 {
		if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{{
			ID:        12,
			Count:     1,
			Exclusive: true,
		}}}, 100, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.itemRequest(c, &protocol.Reader{Data: []byte{7, 1, 0, 0, 3, 0, 0, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	items, err := db.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == 12 && item.Status == 1 && item.InUse == 1 {
			return
		}
	}
	t.Fatal("remaining live weapon copy is unselected and will be omitted from host inventory")
}
