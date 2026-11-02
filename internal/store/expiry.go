package store

// ExpireItems deletes inventory rows whose absolute expiry has passed and
// queues one login notice per expired item. Dormant items store a relative
// duration (1..36500) and are left untouched.
func (s *Store) ExpireItems(now int) (int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT userID,slot,itemID FROM UserInventory WHERE expiryDate>36500 AND expiryDate<=?", now)
	if err != nil {
		return 0, err
	}
	type expired struct {
		user int
		slot int
		item int
	}
	var items []expired
	for rows.Next() {
		var item expired
		if err = rows.Scan(&item.user, &item.slot, &item.item); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		if _, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", item.user, item.slot); err != nil {
			return 0, err
		}
		if _, err = tx.Exec("INSERT INTO UserExpiryNotice(userID,itemID) VALUES(?,?)", item.user, item.item); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(items), nil
}
