package store

import (
	"database/sql"
	"errors"
)

type WeaponReleaseRow struct {
	ItemID   int
	Progress int
	Opened   int
}

type WeaponReleaseCharacter struct {
	Character int
	Count     int
}

func (s *Store) WeaponReleaseRows(userID int) ([]WeaponReleaseRow, error) {
	rows, err := s.DB.Query("SELECT slot,coalesce(character,0),coalesce(opened,0) FROM UserMiniGameWeaponReleaseItemProgress WHERE userID=? ORDER BY slot", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []WeaponReleaseRow
	for rows.Next() {
		var row WeaponReleaseRow
		if err = rows.Scan(&row.ItemID, &row.Progress, &row.Opened); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *Store) WeaponReleaseCharacters(userID int) ([]WeaponReleaseCharacter, int, error) {
	rows, err := s.DB.Query("SELECT character,coalesce(count,0) FROM UserMiniGameWeaponReleaseCharacters WHERE userID=? ORDER BY character", userID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var result []WeaponReleaseCharacter
	total := 0
	for rows.Next() {
		var character string
		var count int
		if err = rows.Scan(&character, &count); err != nil {
			return nil, 0, err
		}
		if len(character) == 0 {
			continue
		}
		result = append(result, WeaponReleaseCharacter{Character: int(character[0]), Count: count})
		total += count
	}
	return result, total, rows.Err()
}

// AddWeaponReleaseCharacter adds letters to the minigame pool; used by the GM
// /addwpnreleasechar command.
func (s *Store) AddWeaponReleaseCharacter(userID int, character byte, count int) error {
	if count < 1 {
		return errors.New("character count must be positive")
	}
	letter := string(rune(character))
	result, err := s.DB.Exec("UPDATE UserMiniGameWeaponReleaseCharacters SET count=count+? WHERE userID=? AND character=?", count, userID, letter)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n > 0 {
		return nil
	}
	_, err = s.DB.Exec("INSERT INTO UserMiniGameWeaponReleaseCharacters(userID,character,count) VALUES(?,?,?)", userID, letter, count)
	return err
}

// SetWeaponReleaseCharacter consumes one letter and sets one progress bit.
// Result codes: -1 row already opened, -2 bit already set, -3 no letter left,
// 0 database error, 1 updated. The caller owns the bit-to-position mapping.
func (s *Store) SetWeaponReleaseCharacter(userID, itemID, bit, character int, opened bool) (int, int, int, error) {
	if bit < 0 || bit > 7 || character < 0 || character > 255 {
		return -3, 0, 0, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback()
	var progress, currentOpened int
	err = tx.QueryRow("SELECT coalesce(character,0),coalesce(opened,0) FROM UserMiniGameWeaponReleaseItemProgress WHERE userID=? AND slot=? LIMIT 1", userID, itemID).Scan(&progress, &currentOpened)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, 0, err
	}
	if currentOpened != 0 {
		return -1, progress, 0, nil
	}
	if progress&(1<<bit) != 0 {
		return -2, progress, 0, nil
	}
	letter := string(rune(character))
	var count int
	err = tx.QueryRow("SELECT coalesce(count,0) FROM UserMiniGameWeaponReleaseCharacters WHERE userID=? AND character=? LIMIT 1", userID, letter).Scan(&count)
	if err != nil && err != sql.ErrNoRows {
		return 0, 0, 0, err
	}
	if count <= 0 {
		return -3, progress, 0, nil
	}
	nextProgress := progress | 1<<bit
	nextOpened := 0
	if opened {
		nextOpened = 1
	}
	result, err := tx.Exec("UPDATE UserMiniGameWeaponReleaseItemProgress SET character=?,opened=? WHERE userID=? AND slot=?", nextProgress, nextOpened, userID, itemID)
	if err != nil {
		return 0, 0, 0, err
	}
	updated, err := result.RowsAffected()
	if err != nil {
		return 0, 0, 0, err
	}
	if updated == 0 {
		if _, err = tx.Exec("INSERT INTO UserMiniGameWeaponReleaseItemProgress(userID,slot,character,opened) VALUES(?,?,?,?)", userID, itemID, nextProgress, nextOpened); err != nil {
			return 0, 0, 0, err
		}
	}
	if _, err = tx.Exec("UPDATE UserMiniGameWeaponReleaseCharacters SET count=count-1 WHERE userID=? AND character=? AND count>0", userID, letter); err != nil {
		return 0, 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, 0, err
	}
	return 1, nextProgress, count - 1, nil
}
