package store

// ExpireItems deletes inventory rows whose absolute expiry has passed and
// queues one login notice per expired item. Dormant items store a relative
// duration (1..36500) and are left untouched.
func (s *Store) ExpireItems(now int) (int, error) {
	removed, _, err := s.ExpireItemsWithUsers(now)
	return removed, err
}

// ExpireItemsWithUsers is ExpireItems plus the affected user IDs, so the caller
// can refresh online clients whose bag or selection changed.
func (s *Store) ExpireItemsWithUsers(now int) (int, []int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT userID,slot,itemID,coalesce(status,0) FROM UserInventory WHERE expiryDate>36500 AND expiryDate<=?", now)
	if err != nil {
		return 0, nil, err
	}
	type expired struct {
		user   int
		slot   int
		item   int
		status int
	}
	var items []expired
	for rows.Next() {
		var item expired
		if err = rows.Scan(&item.user, &item.slot, &item.item, &item.status); err != nil {
			rows.Close()
			return 0, nil, err
		}
		items = append(items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, nil, err
	}
	byUser := map[int][]Item{}
	order := make([]int, 0, len(items))
	for _, item := range items {
		if _, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", item.user, item.slot); err != nil {
			return 0, nil, err
		}
		if _, err = tx.Exec("INSERT INTO UserExpiryNotice(userID,itemID) VALUES(?,?)", item.user, item.item); err != nil {
			return 0, nil, err
		}
		if _, ok := byUser[item.user]; !ok {
			order = append(order, item.user)
		}
		byUser[item.user] = append(byUser[item.user], Item{Slot: item.slot, ID: item.item, Status: item.status})
	}
	for _, userID := range order {
		removed := byUser[userID]
		for _, item := range removed {
			if err = clearItemReference(tx, userID, item.ID); err != nil {
				return 0, nil, err
			}
		}
		if err = promoteItemSelection(tx, userID, removed); err != nil {
			return 0, nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, nil, err
	}
	return len(items), order, nil
}
