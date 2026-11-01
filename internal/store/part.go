package store

import "fmt"

func partColumn(index int) (string, error) {
	if index == 0 {
		return "partSlot1", nil
	}
	if index == 1 {
		return "partSlot2", nil
	}
	return "", ErrItemUnavailable
}

// ApplyWeaponPart attaches a category-13 part to a weapon and consumes the part
// item in one transaction. partSlot is the owned inventory slot of the part.
func (s *Store) ApplyWeaponPart(id, weaponSlot, partSlot, partIndex, partItemID int) ([]Item, error) {
	column, err := partColumn(partIndex)
	if err != nil {
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
	var weapon, part Item
	for _, item := range items {
		if item.Slot == weaponSlot {
			weapon = item
		}
		if item.Slot == partSlot {
			part = item
		}
	}
	if weapon.ID == 0 || part.ID != partItemID || part.Count < 1 || weapon.Slot == part.Slot {
		return nil, ErrItemUnavailable
	}
	if _, err = tx.Exec(fmt.Sprintf("UPDATE UserInventory SET %s=? WHERE userID=? AND slot=?", column), partItemID, id, weapon.Slot); err != nil {
		return nil, err
	}
	removed := part.Count == 1
	if removed {
		_, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, part.Slot)
	} else {
		_, err = tx.Exec("UPDATE UserInventory SET count=count-1 WHERE userID=? AND slot=?", id, part.Slot)
	}
	if err != nil {
		return nil, err
	}
	items, err = readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	if removed {
		items = append([]Item{{Slot: part.Slot}}, items...)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) RemoveWeaponPart(id, weaponSlot, partIndex int) ([]Item, error) {
	column, err := partColumn(partIndex)
	if err != nil {
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
	var weapon Item
	for _, item := range items {
		if item.Slot == weaponSlot {
			weapon = item
			break
		}
	}
	if weapon.ID == 0 {
		return nil, ErrItemUnavailable
	}
	if _, err = tx.Exec(fmt.Sprintf("UPDATE UserInventory SET %s=0 WHERE userID=? AND slot=?", column), id, weapon.Slot); err != nil {
		return nil, err
	}
	if partIndex == 0 {
		weapon.Part1 = 0
	} else {
		weapon.Part2 = 0
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return []Item{weapon}, nil
}
