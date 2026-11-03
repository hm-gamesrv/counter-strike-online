package store

// relativeExpiryMax is the largest expiryDate that still means "days remaining"
// rather than an absolute minute timestamp.
const relativeExpiryMax = 36500

// UseInGameItem applies the default branch of CItemManager::UseItem to one row:
// an item carrying a remaining duration is spent extending another active copy of
// the same item, otherwise the row simply becomes selected. It is deliberately
// separate from ChangeItem: UseItem does not refuse an item whose inUse flag is
// already set, and it never deselects other copies.
func (s *Store) UseInGameItem(id, slot int) ([]Item, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, err := readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	var target Item
	found := false
	for _, item := range items {
		if item.Slot == slot {
			target = item
			found = true
			break
		}
	}
	// CanUseItem: an exhausted or already selected item is refused outright.
	if !found || target.Count <= 0 || target.Status != 0 {
		return nil, ErrItemUnavailable
	}
	now := nowMinutes()
	duration := 0
	if target.Expiry >= 1 && target.Expiry <= relativeExpiryMax {
		duration = target.Expiry * 1440
	}
	if duration > 0 {
		for _, other := range items {
			if other.Slot == target.Slot || other.ID != target.ID || other.Expiry == 0 || other.Status == 0 {
				continue
			}
			// ExtendItem with duration=false: the coupon's remaining time is added
			// to the active copy, then the coupon itself is removed.
			extended := other
			extended.Expiry += duration
			if _, err = tx.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=? AND slot=?", extended.Expiry, id, other.Slot); err != nil {
				return nil, err
			}
			if _, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, target.Slot); err != nil {
				return nil, err
			}
			if err = clearItemReference(tx, id, target.ID); err != nil {
				return nil, err
			}
			if err = tx.Commit(); err != nil {
				return nil, err
			}
			return []Item{{Slot: target.Slot}, extended}, nil
		}
	}
	// No copy to extend: the row becomes selected. A relative duration turns into
	// an absolute timestamp, matching ConvertDurationToExpiryDate + UITEM_FLAG_STATUS.
	update := target
	update.Status = 1
	if duration > 0 {
		update.Expiry = now + duration
	}
	if _, err = tx.Exec("UPDATE UserInventory SET status=1,expiryDate=? WHERE userID=? AND slot=?", update.Expiry, id, update.Slot); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return []Item{update}, nil
}
