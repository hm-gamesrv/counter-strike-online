package store

import (
	"database/sql"
	"errors"
)

// UserExists reports whether a user row exists.
func (s *Store) UserExists(id int) (bool, error) {
	var exists bool
	err := s.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM User WHERE userID=? LIMIT 1)", id).Scan(&exists)
	return exists, err
}

// UserIDByName resolves a login name case-insensitively.
func (s *Store) UserIDByName(name string) (int, error) {
	var id int
	err := s.DB.QueryRow("SELECT userID FROM User WHERE userName=? COLLATE NOCASE", name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, sql.ErrNoRows
	}
	return id, err
}

// UserLastIP returns the IP recorded by the last successful login.
func (s *Store) UserLastIP(id int) (string, error) {
	var ip string
	err := s.DB.QueryRow("SELECT coalesce(lastIP,'') FROM User WHERE userID=?", id).Scan(&ip)
	return ip, err
}

// UserLastHWID returns the HWID recorded by the last successful login.
func (s *Store) UserLastHWID(id int) ([]byte, error) {
	var hwid []byte
	err := s.DB.QueryRow("SELECT coalesce(lastHWID,x'') FROM User WHERE userID=?", id).Scan(&hwid)
	return hwid, err
}

// SetUserBan stores an account ban; days 0 means effectively permanent, matching
// upstream /ban which multiplies the day count by 1440 minutes.
func (s *Store) SetUserBan(id, banType int, reason string, days, now int) error {
	if days == 0 {
		days = 999999
	}
	term := days*24*60 + now
	result, err := s.DB.Exec("UPDATE UserBan SET type=?,reason=?,term=? WHERE userID=?", banType, reason, term, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 0 {
		return nil
	}
	_, err = s.DB.Exec("INSERT INTO UserBan(userID,type,reason,term) VALUES(?,?,?,?)", id, banType, reason, term)
	return err
}

// ClearUserBan removes the account ban row, matching upstream unban.
func (s *Store) ClearUserBan(id int) error {
	_, err := s.DB.Exec("DELETE FROM UserBan WHERE userID=?", id)
	return err
}

// AddIPBan inserts ip into IPBanList; duplicates are ignored.
func (s *Store) AddIPBan(ip string) error {
	_, err := s.DB.Exec("INSERT OR IGNORE INTO IPBanList(ip) VALUES(?)", ip)
	return err
}

// RemoveIPBan deletes ip from IPBanList.
func (s *Store) RemoveIPBan(ip string) error {
	_, err := s.DB.Exec("DELETE FROM IPBanList WHERE ip=?", ip)
	return err
}

// IPBanned reports whether ip is in IPBanList.
func (s *Store) IPBanned(ip string) (bool, error) {
	var banned bool
	err := s.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM IPBanList WHERE ip=? LIMIT 1)", ip).Scan(&banned)
	return banned, err
}

// AddHWIDBan inserts hwid into HWIDBanList; duplicates are ignored.
func (s *Store) AddHWIDBan(hwid []byte) error {
	_, err := s.DB.Exec("INSERT OR IGNORE INTO HWIDBanList(hwid) VALUES(?)", hwid)
	return err
}

// RemoveHWIDBan deletes hwid from HWIDBanList.
func (s *Store) RemoveHWIDBan(hwid []byte) error {
	_, err := s.DB.Exec("DELETE FROM HWIDBanList WHERE hwid=?", hwid)
	return err
}

// HWIDBanned reports whether hwid is in HWIDBanList.
func (s *Store) HWIDBanned(hwid []byte) (bool, error) {
	var banned bool
	err := s.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM HWIDBanList WHERE hwid=? LIMIT 1)", hwid).Scan(&banned)
	return banned, err
}

// AddPoints applies delta to the character's points and returns the new balance.
func (s *Store) AddPoints(id, delta int) (int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE UserCharacter SET points=points+? WHERE userID=?", delta, id); err != nil {
		return 0, err
	}
	var points int
	if err = tx.QueryRow("SELECT points FROM UserCharacter WHERE userID=?", id).Scan(&points); err != nil {
		return 0, err
	}
	return points, tx.Commit()
}
