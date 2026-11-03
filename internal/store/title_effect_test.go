package store

import (
	"sync"
	"testing"
)

// TestSetTitleKeepsConcurrentSlots: the titles column holds every slot in one
// string, so a read-modify-write outside a transaction loses a slot whenever two
// writers interleave (a reconnect taking the account over, or a GM command).
func TestSetTitleKeepsConcurrentSlots(t *testing.T) {
	s := costumeDB(t)
	var wg sync.WaitGroup
	for round := 0; round < 200; round++ {
		for slot := 0; slot < 2; slot++ {
			wg.Add(1)
			go func(slot, round int) {
				defer wg.Done()
				if err := s.SetTitle(1, slot, 100+slot); err != nil {
					t.Errorf("SetTitle slot %d: %v", slot, err)
				}
			}(slot, round)
		}
	}
	wg.Wait()
	character, err := s.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if character.Titles[0] != 100 || character.Titles[1] != 101 {
		t.Fatalf("concurrent title writes lost a slot: %v", character.Titles)
	}
}

// TestToggleNameplateRejectsExpiredRow: an expired row is no longer part of the
// bag, so switching it on would select a nameplate the inventory never shows.
func TestToggleNameplateRejectsExpiredRow(t *testing.T) {
	s := costumeDB(t)
	if _, err := s.GrantReward(1, Grant{Items: []Reward{{
		ID:      7001,
		Count:   1,
		Dormant: true,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=1 AND slot=2", nowMinutes()-1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ToggleNameplate(1, 7001, 2); err == nil {
		t.Fatal("expired inventory row was accepted as a nameplate")
	}
	character, err := s.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if character.NameplateID != 0 {
		t.Fatalf("expired item became the selected nameplate: %d", character.NameplateID)
	}
}
