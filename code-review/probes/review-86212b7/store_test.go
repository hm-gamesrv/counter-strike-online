package store

import (
	"reflect"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func TestReview862PurchasePatchMatchesStoredRow(t *testing.T) {
	for _, stackable := range []bool{false, true} {
		name := "extend"
		if stackable {
			name = "stack"
		}
		t.Run(name, func(t *testing.T) {
			s := testStore(t)
			if err := s.Register("review862", "secret1", "127.0.0.1", 0); err != nil {
				t.Fatal(err)
			}
			if err := s.CreateCharacter(1, "Review862", config.DefaultUser{Level: 1}); err != nil {
				t.Fatal(err)
			}
			reward := Reward{
				ID:         7001,
				Count:      1,
				Duration:   3,
				Unselected: stackable,
				Stackable:  stackable,
			}
			if _, err := s.GrantReward(1, Grant{Items: []Reward{reward}}, 100, 0); err != nil {
				t.Fatal(err)
			}
			if _, err := s.DB.Exec("UPDATE UserInventory SET paintID=744,lockStatus=1 WHERE userID=1 AND itemID=7001"); err != nil {
				t.Fatal(err)
			}
			patch, err := s.PurchaseTracked(1, true, 0, 0, []Reward{reward}, 100, 0)
			if err != nil {
				t.Fatal(err)
			}
			items, err := s.Inventory(1)
			if err != nil {
				t.Fatal(err)
			}
			for _, stored := range items {
				if stored.ID != 7001 {
					continue
				}
				if len(patch) != 1 || !reflect.DeepEqual(patch[0], stored) {
					t.Fatalf("purchase patch differs from persisted row: patch=%+v stored=%+v", patch, stored)
				}
				return
			}
			t.Fatal("missing purchased item")
		})
	}
}
