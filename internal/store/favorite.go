package store

import (
	"errors"
	"slices"
	"strconv"
	"strings"
)

const fastBuySlots = 11

func (s *Store) SetFastBuy(id, slot int, name string, items []int) error {
	if slot < 0 || slot >= 5 || len(name) > 32 || len(items) != fastBuySlots {
		return errors.New("invalid fastbuy preset")
	}
	parts := make([]string, len(items))
	for i, item := range items {
		if item < 0 || item > 65535 {
			return errors.New("invalid fastbuy item")
		}
		parts[i] = strconv.Itoa(item)
	}
	data := strings.Join(parts, ",")
	result, err := s.DB.Exec("UPDATE UserFastBuy SET name=?,items=? WHERE userID=? AND fastBuyID=?", name, data, id, slot)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		_, err = s.DB.Exec("INSERT INTO UserFastBuy(userID,fastBuyID,name,items) VALUES(?,?,?,?)", id, slot, name, data)
		return err
	}
	return nil
}

// SetAddonsConsuming replaces the addon list and consumes one unit of every
// requested item in the same transaction, so a rejected request or a failed save
// cannot eat the player's materials. Missing or protected items are skipped, as
// the reference does per item. protected lists item IDs.
func (s *Store) SetAddonsConsuming(id int, itemIDs []int, protected []int) ([]int, []Item, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	items, err := readInventory(tx, id)
	if err != nil {
		return nil, nil, err
	}
	addons := make([]int, 0, len(itemIDs))
	removed := make([]int, 0, len(itemIDs))
	for _, itemID := range itemIDs {
		target := -1
		for i := range items {
			if items[i].ID == itemID && items[i].Count > 0 && !slices.Contains(protected, items[i].ID) {
				target = i
				break
			}
		}
		if target < 0 {
			continue
		}
		if items[target].Count == 1 {
			result, err := tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, items[target].Slot)
			if err != nil {
				return nil, nil, err
			}
			if n, err := result.RowsAffected(); err != nil {
				return nil, nil, err
			} else if n != 1 {
				return nil, nil, ErrItemUnavailable
			}
			removed = append(removed, items[target].Slot)
			// The row is gone: the snapshot must not offer it to a later request.
			items[target].Count = 0
		} else {
			result, err := tx.Exec("UPDATE UserInventory SET count=count-1 WHERE userID=? AND slot=?", id, items[target].Slot)
			if err != nil {
				return nil, nil, err
			}
			if n, err := result.RowsAffected(); err != nil {
				return nil, nil, err
			} else if n != 1 {
				return nil, nil, ErrItemUnavailable
			}
			items[target].Count--
		}
		addons = append(addons, itemID)
	}
	if _, err = tx.Exec("DELETE FROM UserAddon WHERE userID=?", id); err != nil {
		return nil, nil, err
	}
	for _, addon := range addons {
		if _, err = tx.Exec("INSERT INTO UserAddon(userID,itemID) VALUES(?,?)", id, addon); err != nil {
			return nil, nil, err
		}
	}
	changed, err := readInventory(tx, id)
	if err != nil {
		return nil, nil, err
	}
	for _, slot := range removed {
		changed = append([]Item{{Slot: slot}}, changed...)
	}
	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}
	return addons, changed, nil
}

// SetAddons replaces the character's ZB addon list.
func (s *Store) SetAddons(id int, addons []int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM UserAddon WHERE userID=?", id); err != nil {
		return err
	}
	for _, addon := range addons {
		if _, err = tx.Exec("INSERT INTO UserAddon(userID,itemID) VALUES(?,?)", id, addon); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Addons(id int) ([]int, error) {
	rows, err := s.DB.Query("SELECT itemID FROM UserAddon WHERE userID=? ORDER BY rowid", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var addons []int
	for rows.Next() {
		var item int
		if err = rows.Scan(&item); err != nil {
			return nil, err
		}
		addons = append(addons, item)
	}
	return addons, rows.Err()
}
