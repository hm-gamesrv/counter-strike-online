package store

import "slices"

// ConsumeItem removes one unit of the first owned copy of itemID. Deleted slots
// are reported with a zeroed item so the client can clear them.
func (s *Store) ConsumeItem(id, itemID int, protected []int) ([]Item, error) {
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
	for _, item := range items {
		// protected lists item IDs (default/pseudo-default), not slots.
		if item.ID == itemID && item.Count > 0 && !slices.Contains(protected, item.ID) {
			target = item
			break
		}
	}
	if target.ID == 0 {
		return nil, ErrItemUnavailable
	}
	removed := target.Count == 1
	if removed {
		_, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, target.Slot)
	} else {
		_, err = tx.Exec("UPDATE UserInventory SET count=count-1 WHERE userID=? AND slot=?", id, target.Slot)
	}
	if err != nil {
		return nil, err
	}
	items, err = readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	if removed {
		items = append([]Item{{Slot: target.Slot}}, items...)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}
