package store

import "errors"

// AddBan mirrors the upstream result codes: -1 target character missing,
// -2 already blocked, -3 list full, 1 inserted.
func (s *Store) AddBan(id int, gameName string, max int) (int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM UserCharacter WHERE gameName=? LIMIT 1)", gameName).Scan(&exists); err != nil {
		return 0, err
	}
	if exists == 0 {
		return -1, nil
	}
	if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM UserBanList WHERE userID=? AND gameName=? LIMIT 1)", id, gameName).Scan(&exists); err != nil {
		return 0, err
	}
	if exists != 0 {
		return -2, nil
	}
	var count int
	if err = tx.QueryRow("SELECT count(*) FROM UserBanList WHERE userID=?", id).Scan(&count); err != nil {
		return 0, err
	}
	if count >= max {
		return -3, nil
	}
	if _, err = tx.Exec("INSERT INTO UserBanList(userID,gameName) VALUES(?,?)", id, gameName); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return 1, nil
}

func (s *Store) RemoveBan(id int, gameName string) (int, error) {
	result, err := s.DB.Exec("DELETE FROM UserBanList WHERE userID=? AND gameName=?", id, gameName)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return -1, nil
	}
	return 1, nil
}

func (s *Store) BanList(id int) ([]string, error) {
	rows, err := s.DB.Query("SELECT gameName FROM UserBanList WHERE userID=? ORDER BY rowid", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func (s *Store) IsBanned(id int, gameName string) (bool, error) {
	var found int
	if err := s.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM UserBanList WHERE userID=? AND gameName=? LIMIT 1)", id, gameName).Scan(&found); err != nil {
		return false, err
	}
	return found != 0, nil
}

func (s *Store) BanSettings(id int) (int, error) {
	var settings int
	if err := s.DB.QueryRow("SELECT coalesce(banSettings,2) FROM UserCharacterExtended WHERE userID=?", id).Scan(&settings); err != nil {
		return 0, err
	}
	return settings, nil
}

func (s *Store) SetBanSettings(id, settings int) error {
	if settings < 0 || settings > 255 {
		return errors.New("ban settings outside uint8 range")
	}
	_, err := s.DB.Exec("UPDATE UserCharacterExtended SET banSettings=? WHERE userID=?", settings, id)
	return err
}
