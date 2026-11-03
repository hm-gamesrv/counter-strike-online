package store

import "strings"

// gachaponBonusKills is the count the "kills until a gachapon item" counter is
// reset to after it pays out, as in CGameMatch::OnKillEvent.
const gachaponBonusKills = 100

// CountableWithButton reports whether CItemManager::AddItem treats the item as a
// countable stack with a use button: category 9 with use types 0/1/2, or category
// 8 with use types 0/2. Such items are stored unselected and merge into an
// existing row; equipment is stored selected.
func CountableWithButton(category, useType int) bool {
	return category == 9 && useType >= 0 && useType <= 2 ||
		category == 8 && (useType == 0 || useType == 2)
}

// Equippable reports whether AddItem marks the item exclusive, which deselects
// every other copy of the same item.
func Equippable(category int) bool {
	return category >= 1 && category <= 6 || category == 11
}

// GachaponAward reports what one kill payout produced.
type GachaponAward struct {
	// PasswordBox is set when the prize is item 166, which upstream stores as a
	// character counter instead of an inventory row.
	PasswordBox bool
	// Items holds only the rows this payout touched (a merged stack or a new
	// row), matching the reference's SendInventoryAdd of the added item alone.
	Items []Item
}

// TickGachapon decrements the kill counter of CGameMatch::OnKillEvent and, when it
// reaches zero, resets it and grants the prize in the same transaction: the
// counter and the award must not diverge when the grant fails.
//
// The prize is inserted with the same rules as CItemManager::AddItem, so a
// countable prize lands unselected and merges into an existing stack instead of
// opening a second row.
func (s *Store) TickGachapon(id, award, limit, defaultSlots int) (bool, GachaponAward, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, GachaponAward{}, err
	}
	defer tx.Rollback()
	var remaining int
	if err = tx.QueryRow("SELECT coalesce(killsToGetGachaponItem,0) FROM UserCharacterExtended WHERE userID=?", id).Scan(&remaining); err != nil {
		return false, GachaponAward{}, err
	}
	remaining--
	if remaining > 0 {
		if _, err = tx.Exec("UPDATE UserCharacterExtended SET killsToGetGachaponItem=? WHERE userID=?", remaining, id); err != nil {
			return false, GachaponAward{}, err
		}
		if err = tx.Commit(); err != nil {
			return false, GachaponAward{}, err
		}
		return false, GachaponAward{}, nil
	}
	remaining = gachaponBonusKills
	reward := s.gachaponReward(award)
	touched, err := grantItemsTracked(tx, id, []Reward{reward}, limit, defaultSlots)
	if err != nil {
		if strings.Contains(err.Error(), "INVENTORY_FULL") {
			// A full bag must not break the kill event: leave the counter at its
			// pre-decrement value so the next kill retries the payout.
			return false, GachaponAward{}, nil
		}
		return false, GachaponAward{}, err
	}
	if _, err = tx.Exec("UPDATE UserCharacterExtended SET killsToGetGachaponItem=? WHERE userID=?", remaining, id); err != nil {
		return false, GachaponAward{}, err
	}
	if err = tx.Commit(); err != nil {
		return false, GachaponAward{}, err
	}
	return true, GachaponAward{
		PasswordBox: award == passwordBoxItemID,
		Items:       touched,
	}, nil
}

// passwordBoxItemID is stored as a character counter, not an inventory row.
const passwordBoxItemID = 166

// gachaponReward applies the catalog rules to one prize. The server installs the
// catalog at startup; without it the prize takes the countable shape, which is
// what both gachapon prizes are (the password box never reaches this path).
func (s *Store) gachaponReward(award int) Reward {
	reward := Reward{
		ID:         award,
		Count:      1,
		Unselected: true,
		Stackable:  true,
	}
	if s.ItemRule != nil {
		category, useType := s.ItemRule(award)
		countable := CountableWithButton(category, useType)
		reward.Unselected = countable
		reward.Stackable = countable
		reward.Exclusive = Equippable(category)
	}
	return reward
}
