package store

import "slices"

// DeleteItems removes the selected inventory rows. The upstream disassemble
// handler removes whole items and does not grant any reward, so this mirrors it.
func (s *Store) DeleteItems(id int, slots []int, protected []int) ([]Item, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, err := readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	bySlot := map[int]Item{}
	for _, item := range items {
		bySlot[item.Slot] = item
	}
	removed := make([]int, 0, len(slots))
	seen := map[int]bool{}
	for _, slot := range slots {
		item, ok := bySlot[slot]
		// protected lists item IDs (default/pseudo-default), not slots.
		if !ok || seen[slot] || slices.Contains(protected, item.ID) {
			continue
		}
		seen[slot] = true
		if _, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, slot); err != nil {
			return nil, err
		}
		removed = append(removed, item.Slot)
	}
	if len(removed) == 0 {
		return nil, nil
	}
	items, err = readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	for _, slot := range removed {
		items = append([]Item{{Slot: slot}}, items...)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}
