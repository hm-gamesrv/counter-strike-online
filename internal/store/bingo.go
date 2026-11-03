package store

import "database/sql"

type BingoState struct {
	Status  int
	CanPlay int
}

type BingoSlot struct {
	Number int
	Opened int
}

type BingoPrize struct {
	Index    int
	Opened   int
	ItemID   int
	Count    int
	Duration int
}

func (s *Store) BingoState(userID int) (BingoState, error) {
	var state BingoState
	err := s.DB.QueryRow("SELECT coalesce(status,0),coalesce(canPlay,0) FROM UserMiniGameBingo WHERE userID=? LIMIT 1", userID).Scan(&state.Status, &state.CanPlay)
	return state, err
}

func (s *Store) BingoSlots(userID int) ([]BingoSlot, error) {
	rows, err := s.DB.Query("SELECT number,coalesce(opened,0) FROM UserMiniGameBingoSlot WHERE userID=? ORDER BY rowid", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var slots []BingoSlot
	for rows.Next() {
		var slot BingoSlot
		if err = rows.Scan(&slot.Number, &slot.Opened); err != nil {
			return nil, err
		}
		slots = append(slots, slot)
	}
	return slots, rows.Err()
}

func (s *Store) BingoPrizes(userID int) ([]BingoPrize, error) {
	rows, err := s.DB.Query("SELECT idx,coalesce(opened,0),coalesce(itemID,0),coalesce(count,0),coalesce(duration,0) FROM UserMiniGameBingoPrizeSlot WHERE userID=? ORDER BY idx", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var prizes []BingoPrize
	for rows.Next() {
		var prize BingoPrize
		if err = rows.Scan(&prize.Index, &prize.Opened, &prize.ItemID, &prize.Count, &prize.Duration); err != nil {
			return nil, err
		}
		prizes = append(prizes, prize)
	}
	return prizes, rows.Err()
}

// InitBingo replaces the desk and prize slots and marks the event in progress.
func (s *Store) InitBingo(userID int, slots []BingoSlot, prizes []BingoPrize) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = replaceBingo(tx, userID, slots, prizes); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func replaceBingo(tx *sql.Tx, userID int, slots []BingoSlot, prizes []BingoPrize) error {
	if _, err := tx.Exec("DELETE FROM UserMiniGameBingoSlot WHERE userID=?", userID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM UserMiniGameBingoPrizeSlot WHERE userID=?", userID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM UserMiniGameBingo WHERE userID=?", userID); err != nil {
		return err
	}
	for _, slot := range slots {
		if _, err := tx.Exec("INSERT INTO UserMiniGameBingoSlot(userID,number,opened) VALUES(?,?,?)", userID, slot.Number, slot.Opened); err != nil {
			return err
		}
	}
	for _, prize := range prizes {
		if _, err := tx.Exec("INSERT INTO UserMiniGameBingoPrizeSlot(userID,idx,opened,itemID,count,duration) VALUES(?,?,?,?,?,?)", userID, prize.Index, prize.Opened, prize.ItemID, prize.Count, prize.Duration); err != nil {
			return err
		}
	}
	_, err := tx.Exec("INSERT INTO UserMiniGameBingo(userID,status,canPlay) VALUES(?,1,1)", userID)
	return err
}

func (s *Store) SetBingoSlots(userID int, slots []BingoSlot) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM UserMiniGameBingoSlot WHERE userID=?", userID); err != nil {
		return err
	}
	for _, slot := range slots {
		if _, err = tx.Exec("INSERT INTO UserMiniGameBingoSlot(userID,number,opened) VALUES(?,?,?)", userID, slot.Number, slot.Opened); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// OpenBingoAwards marks the opened number, the completed prize lines and the
// prize items in one transaction, so a full bag can neither pay one prize of a
// line nor leave the line unclaimed.
func (s *Store) OpenBingoAwards(userID, number int, prizes []int, rewards []Reward, limit, defaultSlots int) ([]Item, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE UserMiniGameBingoSlot SET opened=1 WHERE userID=? AND number=?", userID, number); err != nil {
		return nil, err
	}
	for _, index := range prizes {
		if _, err = tx.Exec("UPDATE UserMiniGameBingoPrizeSlot SET opened=1 WHERE userID=? AND idx=?", userID, index); err != nil {
			return nil, err
		}
	}
	var items []Item
	if len(rewards) > 0 {
		if err = grantItems(tx, userID, rewards, limit, defaultSlots); err != nil {
			return nil, err
		}
		if items, err = readInventory(tx, userID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

// OpenBingo marks one number and any completed prize lines in one transaction.
func (s *Store) OpenBingo(userID, number int, prizes []int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE UserMiniGameBingoSlot SET opened=1 WHERE userID=? AND number=?", userID, number); err != nil {
		return err
	}
	for _, index := range prizes {
		if _, err = tx.Exec("UPDATE UserMiniGameBingoPrizeSlot SET opened=1 WHERE userID=? AND idx=?", userID, index); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ResetBingo(userID int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, table := range []string{"UserMiniGameBingoSlot", "UserMiniGameBingoPrizeSlot", "UserMiniGameBingo"} {
		if _, err = tx.Exec("DELETE FROM "+table+" WHERE userID=?", userID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
