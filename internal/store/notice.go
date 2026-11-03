package store

func (s *Store) ExpiryNotices(id int) ([]int, error) {
	rows, err := s.DB.Query("SELECT itemID FROM UserExpiryNotice WHERE userID=? ORDER BY itemID", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []int
	for rows.Next() {
		var item int
		if err = rows.Scan(&item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ClearExpiryNotices removes exactly the rows that were delivered. The client
// accepts at most 255 entries per packet, so a long queue must survive until the
// next login instead of being dropped.
func (s *Store) ClearExpiryNotices(id, delivered int) error {
	if delivered <= 0 {
		return nil
	}
	_, err := s.DB.Exec("DELETE FROM UserExpiryNotice WHERE rowid IN (SELECT rowid FROM UserExpiryNotice WHERE userID=? ORDER BY rowid LIMIT ?)", id, delivered)
	return err
}

func (s *Store) RewardNotices(id int) ([]int, error) {
	rows, err := s.DB.Query("SELECT rewardID FROM UserRewardNotice WHERE userID=? ORDER BY rewardID", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var rewards []int
	for rows.Next() {
		var reward int
		if err = rows.Scan(&reward); err != nil {
			return nil, err
		}
		rewards = append(rewards, reward)
	}
	return rewards, rows.Err()
}

func (s *Store) DeleteRewardNotice(id, reward int) error {
	_, err := s.DB.Exec("DELETE FROM UserRewardNotice WHERE rowid IN (SELECT rowid FROM UserRewardNotice WHERE userID=? AND rewardID=? LIMIT 1)", id, reward)
	return err
}
