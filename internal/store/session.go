package store

import "database/sql"

// archiveSession copies the live session into UserSessionHistory exactly the way
// CUserDatabaseSQLite::DropSession does (src/manager/userdatabase_sqlite.cpp:452):
// the history row is stamped with User.lastLogonTime rather than with the current
// time, and keeps the raw sessionTime.
func archiveSession(tx *sql.Tx, id int) error {
	_, err := tx.Exec("INSERT INTO UserSessionHistory(userID,ip,hwid,sessionDate,sessionTime) SELECT s.userID,s.ip,s.hwid,(SELECT lastLogonTime FROM User WHERE userID=s.userID LIMIT 1),s.sessionTime FROM UserSession s WHERE s.userID=?", id)
	return err
}

// StartSession replaces any live session for the account. A second login
// destroys the previous CUser in the reference, and its destructor archives the
// session before the new row is inserted, so the takeover writes history too.
func (s *Store) StartSession(id int, ip string, hwid []byte, now int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = archiveSession(tx, id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM UserSession WHERE userID=?", id); err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO UserSession(userID,ip,uuid,hwid,status,sessionTime) VALUES(?,?,?,?,0,?)", id, ip, "", hwid, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) EndSession(id int) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = archiveSession(tx, id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM UserSession WHERE userID=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
