package store

func (s *Store) StartSession(id int, ip string, hwid []byte, now int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM UserSession WHERE userID=?", id); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO UserSession(userID,ip,uuid,hwid,status,sessionTime) VALUES(?,?,?,?,0,?)", id, ip, "", hwid, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EndSession(id, now int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT INTO UserSessionHistory(userID,ip,hwid,sessionDate,sessionTime) SELECT userID,ip,hwid,?,CASE WHEN sessionTime>0 AND sessionTime<=? THEN ?-sessionTime ELSE 0 END FROM UserSession WHERE userID=?", now, now, now, id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM UserSession WHERE userID=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
