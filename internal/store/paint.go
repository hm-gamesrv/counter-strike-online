package store

import (
	"strconv"
	"strings"
)

func encodePaints(paints []int) string {
	parts := make([]string, len(paints))
	for i, paint := range paints {
		parts[i] = strconv.Itoa(paint)
	}
	return strings.Join(parts, ",")
}

func hasPaint(paints []int, paint int) bool {
	for _, existing := range paints {
		if existing == paint {
			return true
		}
	}
	return false
}

// ApplyWeaponPaint attaches a paint item to a weapon, updates the weapon's
// paint list and consumes one paint item in one transaction.
func (s *Store) ApplyWeaponPaint(id, weaponSlot, paintSlot, paintID int) ([]Item, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, err := readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	var weapon, paint Item
	for _, item := range items {
		if item.Slot == weaponSlot {
			weapon = item
		}
		if item.Slot == paintSlot {
			paint = item
		}
	}
	if weapon.ID == 0 || paint.ID != paintID || paint.Count < 1 || weapon.Slot == paint.Slot || hasPaint(weapon.Paints, paintID) {
		return nil, ErrItemUnavailable
	}
	weapon.Paint = paintID
	weapon.Paints = append(weapon.Paints, paintID)
	if _, err = tx.Exec("UPDATE UserInventory SET paintID=?,paintIDList=? WHERE userID=? AND slot=?", weapon.Paint, encodePaints(weapon.Paints), id, weapon.Slot); err != nil {
		return nil, err
	}
	removed := paint.Count == 1
	if removed {
		_, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, paint.Slot)
	} else {
		_, err = tx.Exec("UPDATE UserInventory SET count=count-1 WHERE userID=? AND slot=?", id, paint.Slot)
	}
	if err != nil {
		return nil, err
	}
	items, err = readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	if removed {
		items = append([]Item{{Slot: paint.Slot}}, items...)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Store) SwitchWeaponPaint(id, weaponSlot, paintID int) ([]Item, error) {
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
	if weapon.ID == 0 || paintID != 0 && !hasPaint(weapon.Paints, paintID) {
		return nil, ErrItemUnavailable
	}
	if _, err = tx.Exec("UPDATE UserInventory SET paintID=? WHERE userID=? AND slot=?", paintID, id, weapon.Slot); err != nil {
		return nil, err
	}
	weapon.Paint = paintID
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return []Item{weapon}, nil
}
