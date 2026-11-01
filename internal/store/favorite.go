package store

import (
	"errors"
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
