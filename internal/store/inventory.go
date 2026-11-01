package store

import (
	"errors"
	"time"
)

// Public client builds and the upstream v4 database use Unix minutes for
// inventory, account and ban dates. Go-only settlement audit dates use seconds.
func nowMinutes() int { return int(time.Now().Unix() / 60) }

type ItemAction int

const (
	SelectItem ItemAction = iota
	ActivateItem
	ToggleItemLock
)

var ErrItemUnavailable = errors.New("Item unavailable.")
var ErrItemProtected = errors.New("This item is protected.")

// ChangeItem checks ownership and expiry inside the transaction, and switches
// same-ID selection atomically. Retried activation never extends the lifetime.
func (s *Store) ChangeItem(id, slot int, action ItemAction, protected []int) ([]Item, error) {
	if slot <= 0 {
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
	target := -1
	for i := range items {
		if items[i].Slot == slot {
			target = i
			break
		}
	}
	if target < 0 {
		return nil, ErrItemUnavailable
	}
	item := items[target]
	var changed []Item
	switch action {
	case ToggleItemLock:
		if item.Lock == 2 {
			return nil, ErrItemProtected
		}
		for _, protectedID := range protected {
			if item.ID == protectedID {
				return nil, ErrItemProtected
			}
		}
		item.Lock = 1 - min(max(item.Lock, 0), 1)
	case SelectItem, ActivateItem:
		if item.InUse == 0 && item.Status == 0 && item.Expiry > 0 && item.Expiry <= 36500 {
			item.Expiry = nowMinutes() + item.Expiry*1440
		}
		if action == ActivateItem && item.InUse != 0 {
			return []Item{item}, nil
		}
		item.InUse = 1
		item.Status = 1
		for i, other := range items {
			if i == target || other.ID != item.ID || other.Status == 0 {
				continue
			}
			if action == ActivateItem {
				item.Status = 0
			} else {
				other.Status = 0
				changed = append(changed, other)
			}
		}
	default:
		return nil, errors.New("invalid item action")
	}
	changed = append(changed, item)
	for _, i := range changed {
		if _, err = tx.Exec("UPDATE UserInventory SET status=?,inUse=?,expiryDate=?,lockStatus=? WHERE userID=? AND slot=?", i.Status, i.InUse, i.Expiry, i.Lock, id, i.Slot); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return changed, nil
}
