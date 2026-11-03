package store

import "strings"

// gachaponBonusKills is the count the "kills until a gachapon item" counter is
// reset to after it pays out, as in CGameMatch::OnKillEvent.
const gachaponBonusKills = 100

// TickGachapon decrements the kill counter of CGameMatch::OnKillEvent and, when it
// reaches zero, resets it and grants the prize in the same transaction: the
// counter and the award must not diverge when the grant fails.
func (s *Store) TickGachapon(id, award, limit, defaultSlots int) (bool, []Item, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return false, nil, err
	}
	defer tx.Rollback()
	var remaining int
	if err = tx.QueryRow("SELECT coalesce(killsToGetGachaponItem,0) FROM UserCharacterExtended WHERE userID=?", id).Scan(&remaining); err != nil {
		return false, nil, err
	}
	remaining--
	awarded := false
	var changed []Item
	if remaining <= 0 {
		remaining = gachaponBonusKills
		err = grantItems(tx, id, []Reward{{
			ID:    award,
			Count: 1,
		}}, limit, defaultSlots)
		if err != nil && strings.Contains(err.Error(), "INVENTORY_FULL") {
			// A full bag must not break the kill event: leave the counter at its
			// pre-decrement value so the next kill retries the payout.
			return false, nil, nil
		}
		if err != nil {
			return false, nil, err
		}
		awarded = true
		if changed, err = readInventory(tx, id); err != nil {
			return false, nil, err
		}
	}
	if _, err = tx.Exec("UPDATE UserCharacterExtended SET killsToGetGachaponItem=? WHERE userID=?", remaining, id); err != nil {
		return false, nil, err
	}
	if err = tx.Commit(); err != nil {
		return false, nil, err
	}
	return awarded, changed, nil
}
