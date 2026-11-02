package store

// EnhanceItem consumes one unit of every material and writes the new
// enhancement state in a single transaction.
func (s *Store) EnhanceItem(id, targetSlot int, materialSlots []int, level, exp, value int) ([]Item, error) {
	if targetSlot <= 0 || level < 0 || level > 9 || exp < 0 || value < 0 || value > 0x0FFFFFFF {
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
	bySlot := map[int]Item{}
	for _, item := range items {
		bySlot[item.Slot] = item
	}
	if _, ok := bySlot[targetSlot]; !ok {
		return nil, ErrItemUnavailable
	}
	seen := map[int]bool{targetSlot: true}
	var removed []int
	for _, slot := range materialSlots {
		material, ok := bySlot[slot]
		if !ok || material.Count < 1 {
			return nil, ErrItemUnavailable
		}
		if seen[slot] {
			return nil, ErrItemUnavailable
		}
		seen[slot] = true
		if material.Count == 1 {
			if _, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, slot); err != nil {
				return nil, err
			}
			removed = append(removed, slot)
		} else if _, err = tx.Exec("UPDATE UserInventory SET count=count-1 WHERE userID=? AND slot=?", id, slot); err != nil {
			return nil, err
		}
	}
	result, err := tx.Exec("UPDATE UserInventory SET enhancementLevel=?,enhancementExp=?,enhanceValue=? WHERE userID=? AND slot=?", level, exp, value, id, targetSlot)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected != 1 {
		return nil, ErrItemUnavailable
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
