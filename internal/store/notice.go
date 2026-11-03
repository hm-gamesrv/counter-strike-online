package store

// ExpiryNotice is one queued "item expired" row. The rowid identifies the exact
// record so the sender can delete only what it actually delivered.
type ExpiryNotice struct {
	RowID  int
	ItemID int
}

// ExpiryNoticeRows returns the queued notices in insertion order.
func (s *Store) ExpiryNoticeRows(id int) ([]ExpiryNotice, error) {
	rows, err := s.DB.Query("SELECT rowid,itemID FROM UserExpiryNotice WHERE userID=? ORDER BY rowid", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var notices []ExpiryNotice
	for rows.Next() {
		var notice ExpiryNotice
		if err = rows.Scan(&notice.RowID, &notice.ItemID); err != nil {
			return nil, err
		}
		notices = append(notices, notice)
	}
	return notices, rows.Err()
}

// ExpiryNotices returns just the item IDs, for callers that only display them.
func (s *Store) ExpiryNotices(id int) ([]int, error) {
	rows, err := s.ExpiryNoticeRows(id)
	if err != nil {
		return nil, err
	}
	items := make([]int, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.ItemID)
	}
	return items, nil
}

// ClearExpiryNotices removes exactly the delivered records by identity. The
// client accepts at most 255 entries per packet, so a longer queue must keep the
// rows that were not sent.
func (s *Store) ClearExpiryNotices(id int, rowIDs []int) error {
	if len(rowIDs) == 0 {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, rowID := range rowIDs {
		if _, err = tx.Exec("DELETE FROM UserExpiryNotice WHERE userID=? AND rowid=?", id, rowID); err != nil {
			return err
		}
	}
	return tx.Commit()
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
