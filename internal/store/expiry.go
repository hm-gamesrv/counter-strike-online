package store

// ExpiredUser reports one account whose bag changed during an expiry sweep.
// Items uses the same slot-keyed update list the other removal paths return: a
// tombstone for every deleted slot followed by the remaining inventory. The
// tombstones matter because the inventory update frame is an incremental,
// slot-keyed patch: resending only what is left never tells the client that a
// slot is gone.
type ExpiredUser struct {
	UserID int
	Items  []Item
}

// ExpireItems deletes inventory rows whose absolute expiry has passed and
// queues one login notice per expired item. Dormant items store a relative
// duration (1..36500) and are left untouched.
func (s *Store) ExpireItems(now int) (int, error) {
	removed, _, err := s.ExpireItemsChanged(now)
	return removed, err
}

// ExpireItemsWithUsers is ExpireItems plus the affected user IDs.
func (s *Store) ExpireItemsWithUsers(now int) (int, []int, error) {
	removed, users, err := s.ExpireItemsChanged(now)
	ids := make([]int, 0, len(users))
	for _, user := range users {
		ids = append(ids, user.UserID)
	}
	return removed, ids, err
}

// ExpireItemsChanged is ExpireItems plus per-user bag updates, so an online
// client can be told which slots disappeared and which remaining copies took
// over a selection. Expired rows also drop external references (nameplate,
// effects, costumes, zombie skins) and promote another live copy when the
// selected weapon expired.
func (s *Store) ExpireItemsChanged(now int) (int, []ExpiredUser, error) {
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
	users := make([]ExpiredUser, 0, len(order))
	for _, userID := range order {
		remaining, err := readInventory(tx, userID)
		if err != nil {
			return 0, nil, err
		}
		changed := make([]Item, 0, len(byUser[userID])+len(remaining))
		for _, item := range byUser[userID] {
			changed = append(changed, Item{Slot: item.Slot})
		}
		changed = append(changed, remaining...)
		users = append(users, ExpiredUser{UserID: userID, Items: changed})
	}
	if err = tx.Commit(); err != nil {
		return 0, nil, err
	}
	return len(items), users, nil
}
