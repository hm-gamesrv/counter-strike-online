package store

// ExtendItem applies one ItemExtend request (extender item 676). The target's
// expiry is updated and the extender stack is consumed in one transaction.
func (s *Store) ExtendItem(id, targetSlot, extenderID, days int) ([]Item, error) {
	if targetSlot <= 0 || days < 1 || days > 36500 {
		return nil, ErrItemUnavailable
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, err := readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	var target, extender Item
	for _, item := range items {
		if item.Slot == targetSlot {
			target = item
		}
		if extender.ID == 0 && item.ID == extenderID {
			extender = item
		}
	}
	if target.ID == 0 || target.Expiry == 0 || extender.ID == 0 || extender.Count < days || target.Slot == extender.Slot {
		return nil, ErrItemUnavailable
	}
	// Only a not-yet-activated copy keeps a relative day count; an activated item
	// keeps an absolute minute timestamp even after being unequipped.
	relative := target.Status == 0 && target.InUse == 0 && target.Expiry > 0 && target.Expiry <= 36500
	if relative {
		target.Expiry += days
	} else if days+(target.Expiry-target.Obtain)/1440 >= 999 {
		target.Expiry = 0
	} else {
		target.Expiry += days * 1440
	}
	removed := extender.Count == days
	if removed {
		_, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, extender.Slot)
	} else {
		_, err = tx.Exec("UPDATE UserInventory SET count=count-? WHERE userID=? AND slot=?", days, id, extender.Slot)
	}
	if err != nil {
		return nil, err
	}
	if _, err = tx.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=? AND slot=?", target.Expiry, id, target.Slot); err != nil {
		return nil, err
	}
	items, err = readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	if removed {
		items = append([]Item{{Slot: extender.Slot}}, items...)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}
