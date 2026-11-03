package store

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

// reviewHeadUser registers an account and its character on the probe store.
func reviewHeadUser(t *testing.T, s *Store, name string) int {
	t.Helper()
	if err := s.Register(name, "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	var id int
	if err := s.DB.QueryRow("SELECT userID FROM User WHERE userName=?", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(id, name, config.DefaultUser{Level: 1, Points: 40000}); err != nil {
		t.Fatal(err)
	}
	return id
}

// The reference's CItemManager::AddItem extends the existing timed copy of the
// same item instead of opening a second row, and returns before inserting
// anything (itemmanager.cpp:716-736). Buying a timed item a second time must
// therefore leave a single row whose expiry moved by the bought duration.
func TestReviewHeadTimedPurchaseExtendsExistingCopy(t *testing.T) {
	s := testStore(t)
	id := reviewHeadUser(t, s, "buyer")
	expiry := nowMinutes() + 3*1440
	if _, err := s.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,1,212,1,1,1,?,?,0,0,0,0,0,0,0,0)", id, nowMinutes(), expiry); err != nil {
		t.Fatal(err)
	}
	if err := s.Purchase(id, true, 1, 0, []Reward{{
		ID:        212,
		Count:     1,
		Duration:  3,
		Exclusive: true,
	}}, 3000, 0); err != nil {
		t.Fatal(err)
	}
	items, err := s.Inventory(id)
	if err != nil {
		t.Fatal(err)
	}
	copies := make([]Item, 0, 2)
	for _, item := range items {
		if item.ID == 212 {
			copies = append(copies, item)
		}
	}
	if len(copies) != 1 {
		t.Fatalf("second purchase of a timed item opened %d rows; the reference extends the existing copy (itemmanager.cpp:721-735)", len(copies))
	}
	if got := copies[0].Expiry; got < expiry+3*1440-2 || got > expiry+3*1440+2 {
		t.Fatalf("extended expiry = %d, want about %d", got, expiry+3*1440)
	}
}

// The reference lets any member whose grade is within the page's access grade
// delete a clan storage entry (userdatabase_sqlite.cpp:4690-4713 checks only the
// access grade). The probe deposits an item with the master and deletes it as a
// plain member on a page everyone may write.
func TestReviewHeadClanStorageMemberDelete(t *testing.T) {
	s := testStore(t)
	master := reviewHeadUser(t, s, "master")
	member := reviewHeadUser(t, s, "member")
	clanID, err := s.CreateClan(ClanCreate{MasterID: master, Name: "Alpha", JoinMethod: 3}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, err := s.JoinClan(member, clanID, 1000); err != nil || code != 1 {
		t.Fatalf("join %d, %v", code, err)
	}
	if _, err = s.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,40,270,1,1,0,?,0,0,0,0,0,0,0,0,0)", master, nowMinutes()); err != nil {
		t.Fatal(err)
	}
	if _, code, err := s.GiveClanItem(master, 0, 40); err != nil || code != 1 {
		t.Fatalf("give %d, %v", code, err)
	}
	if code, err := s.DeleteClanItem(member, 0, 0); err != nil || code != 1 {
		t.Fatalf("member delete = %d, %v; the reference only checks the page access grade", code, err)
	}
}
