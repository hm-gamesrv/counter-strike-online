package store

// OpenItemBox consumes decoder keys from one owned slot and grants the complete
// rolled bundle in the same transaction. awards is server-generated; clients
// never supply it. The returned slice contains a zero-ID placeholder for the
// consumed slot when the key stack is exhausted, which the inventory delta
// encoder turns into a remove entry.
func (s *Store) OpenItemBox(id, slot, itemID, count int, awards []Reward, limit, defaultSlots int) ([]Item, error) {
	if slot <= 0 || itemID <= 0 || count < 1 || len(awards) != count {
		return nil, ErrItemUnavailable
	}
	if err := validateGrants(awards, limit, defaultSlots); err != nil {
		return nil, err
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
	var source Item
	for _, item := range items {
		if item.Slot == slot {
			source = item
			break
		}
	}
	if source.ID != itemID || source.Count < count || source.Lock != 0 {
		return nil, ErrItemUnavailable
	}
	removed := source.Count == count
	if removed {
		_, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, slot)
	} else {
		_, err = tx.Exec("UPDATE UserInventory SET count=count-? WHERE userID=? AND slot=?", count, id, slot)
	}
	if err != nil {
		return nil, err
	}
	if err = grantItems(tx, id, awards, limit, defaultSlots); err != nil {
		return nil, err
	}
	items, err = readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	if removed {
		items = append([]Item{{Slot: slot}}, items...)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}
