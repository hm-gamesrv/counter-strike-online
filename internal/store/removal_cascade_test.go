package store

import "testing"

func addRemovalRow(t *testing.T, s *Store, user, slot, item, status, inUse, count int) {
	t.Helper()
	_, err := s.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,?,?,?,?,?,?,0,0,0,0,0,0,0,0,0)", user, slot, item, count, status, inUse, nowMinutes())
	if err != nil {
		t.Fatal(err)
	}
}

// TestClanDepositClearsEquippedNameplate: depositing a stack leaves the bag, so a
// nameplate pointing at it must be cleared. Nothing rejects an equipped item on
// deposit, which makes this reachable from the storage UI.
func TestClanDepositClearsEquippedNameplate(t *testing.T) {
	s := testStore(t)
	master := clanStorageUser(t, s, "depositor", 40000)
	if _, err := s.DB.Exec("UPDATE UserCharacter SET nameplateID=7001 WHERE userID=?", master); err != nil {
		t.Fatal(err)
	}
	addRemovalRow(t, s, master, 40, 7001, 1, 1, 1)
	if _, err := s.CreateClan(ClanCreate{
		MasterID:   master,
		Name:       "DepositClan",
		JoinMethod: 3,
	}, 1000); err != nil {
		t.Fatal(err)
	}
	if _, code, err := s.GiveClanItem(master, 0, 40); err != nil || code != 1 {
		t.Fatalf("give item code=%d error=%v", code, err)
	}
	character, err := s.Character(master)
	if err != nil {
		t.Fatal(err)
	}
	if character.NameplateID != 0 {
		t.Fatalf("deposited item is still selected: %d", character.NameplateID)
	}
}

// TestEnhanceMaterialPromotesSelectedCopy: materials are ordinary inventory rows,
// so a material can be the selected copy of a weapon. Removing it has to hand the
// selection to a remaining copy, exactly like disassemble and expiry.
func TestEnhanceMaterialPromotesSelectedCopy(t *testing.T) {
	s := testStore(t)
	user := clanStorageUser(t, s, "enhancer", 0)
	// Slots 1..2 hold the character's default items, so use free slots.
	addRemovalRow(t, s, user, 5, 12, 0, 0, 1)
	addRemovalRow(t, s, user, 6, 12, 1, 1, 1)
	addRemovalRow(t, s, user, 7, 12, 0, 1, 1)
	if _, err := s.EnhanceItem(user, 5, []int{6}, 1, 0, 0); err != nil {
		t.Fatal(err)
	}
	var status int
	if err := s.DB.QueryRow("SELECT status FROM UserInventory WHERE userID=? AND slot=7", user).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != 1 {
		t.Fatalf("remaining copy stayed unselected: status=%d", status)
	}
}
