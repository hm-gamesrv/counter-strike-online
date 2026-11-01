package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
)

var ErrRewardClaimed = errors.New("Reward is unavailable or already claimed.")

type RewardClaim struct {
	Token, RewardID int
	Payload         []byte
}

func retireStaleRewardClaims(q interface {
	Exec(string, ...any) (sql.Result, error)
}, id int) error {
	_, err := q.Exec(`UPDATE GoRewardClaim SET claimed=2 WHERE userID=? AND claimed=0 AND sourceSlot>0 AND NOT EXISTS(SELECT 1 FROM UserInventory i WHERE i.userID=GoRewardClaim.userID AND i.slot=GoRewardClaim.sourceSlot AND i.count>0 AND (i.expiryDate=0 OR i.expiryDate>?))`, id, nowMinutes())
	return err
}

func (s *Store) initRewardClaims() error {
	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS GoRewardClaim(userID INT NOT NULL,token INT NOT NULL,rewardID INT NOT NULL,sourceSlot INT NOT NULL,payload BLOB NOT NULL,claimed INT NOT NULL DEFAULT 0,PRIMARY KEY(userID,token));
CREATE UNIQUE INDEX IF NOT EXISTS GoRewardClaimPendingSlot ON GoRewardClaim(userID,sourceSlot) WHERE claimed=0;
CREATE TABLE IF NOT EXISTS GoQuestTaskProgress(userID INT NOT NULL REFERENCES UserCharacter(userID) ON DELETE CASCADE,questID INT NOT NULL,taskID INT NOT NULL,units INT NOT NULL DEFAULT 0,goal INT NOT NULL,token INT NOT NULL DEFAULT 0,PRIMARY KEY(userID,questID,taskID));
CREATE TABLE IF NOT EXISTS GoQuestEvent(userID INT NOT NULL REFERENCES UserCharacter(userID) ON DELETE CASCADE,questID INT NOT NULL,taskID INT NOT NULL,eventKey TEXT NOT NULL,PRIMARY KEY(userID,questID,taskID,eventKey));
CREATE TABLE IF NOT EXISTS GoQuestEventOutbox(id INTEGER PRIMARY KEY AUTOINCREMENT,userID INT NOT NULL REFERENCES UserCharacter(userID) ON DELETE CASCADE,kind INT NOT NULL,count INT NOT NULL,mode INT NOT NULL,mapID INT NOT NULL,players INT NOT NULL,won BOOLEAN NOT NULL);`)
	return err
}

// PrepareRewardClaim freezes all outcomes before an award is attempted. Reusing
// the same owned slot resumes its pending claim, including after process restart.
// No source is consumed until GrantReward commits the claim and awards together.
func (s *Store) PrepareRewardClaim(id, rewardID int, g Grant, payload []byte) (RewardClaim, error) {
	var claim RewardClaim
	if id < 1 || rewardID < 1 || g.SourceSlot < 1 || g.SourceItem < 1 || g.Consume < 1 || g.Consume > 200 || len(payload) == 0 || len(payload) > 1048576 {
		return claim, ErrItemUnavailable
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return claim, err
	}
	defer tx.Rollback()
	if err = retireStaleRewardClaims(tx, id); err != nil {
		return claim, err
	}
	err = tx.QueryRow("SELECT token,rewardID,payload FROM GoRewardClaim WHERE userID=? AND sourceSlot=? AND claimed=0", id, g.SourceSlot).Scan(&claim.Token, &claim.RewardID, &claim.Payload)
	if err == nil {
		return claim, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return claim, err
	}
	var level, pending int
	if err = tx.QueryRow("SELECT level FROM UserCharacter WHERE userID=?", id).Scan(&level); err != nil {
		return claim, err
	}
	if level < g.MinimumLevel {
		return claim, ErrRewardLevel
	}
	owned, err := readInventory(tx, id)
	if err != nil {
		return claim, err
	}
	var source Item
	for _, item := range owned {
		if item.Slot == g.SourceSlot {
			source = item
			break
		}
	}
	if source.ID != g.SourceItem || source.Count < g.Consume || source.InUse != 1 || source.Status != 0 {
		return claim, ErrItemUnavailable
	}
	if source.Lock != 0 {
		return claim, ErrItemProtected
	}
	for _, itemID := range g.Protected {
		if source.ID == itemID {
			return claim, ErrItemProtected
		}
	}
	if err = tx.QueryRow("SELECT count(*) FROM GoRewardClaim WHERE userID=? AND claimed=0", id).Scan(&pending); err != nil {
		return claim, err
	}
	if pending >= 8 {
		return claim, errors.New("finish your pending reward claims first")
	}
	// Tokens are echoed by the current client's selection message. Use a unique
	// token per claim rather than the static catalog ID to reject stale replays.
	claim.Token, err = newRewardToken(tx, id)
	if err != nil {
		return claim, err
	}
	claim.RewardID = rewardID
	claim.Payload = append([]byte(nil), payload...)
	if _, err = tx.Exec("INSERT INTO GoRewardClaim(userID,token,rewardID,sourceSlot,payload) VALUES(?,?,?,?,?)", id, claim.Token, rewardID, g.SourceSlot, payload); err != nil {
		return claim, err
	}
	if err = tx.Commit(); err != nil {
		return claim, err
	}
	return claim, nil
}

func newRewardToken(tx *sql.Tx, id int) (int, error) {
	for {
		var raw [4]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return 0, err
		}
		token := int(binary.LittleEndian.Uint32(raw[:]))
		if token == 0 {
			continue
		}
		var exists int
		if err := tx.QueryRow("SELECT count(*) FROM GoRewardClaim WHERE userID=? AND token=?", id, token).Scan(&exists); err != nil {
			return 0, err
		}
		if exists == 0 {
			return token, nil
		}
	}
}

func (s *Store) PendingRewardClaim(id, token int) (RewardClaim, error) {
	var claim RewardClaim
	err := s.DB.QueryRow("SELECT token,rewardID,payload FROM GoRewardClaim WHERE userID=? AND token=? AND claimed=0", id, token).Scan(&claim.Token, &claim.RewardID, &claim.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrRewardClaimed
	}
	return claim, err
}

func (s *Store) PendingRewardClaims(id int) ([]RewardClaim, error) {
	if err := retireStaleRewardClaims(s.DB, id); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query("SELECT token,rewardID,payload FROM GoRewardClaim WHERE userID=? AND claimed=0 ORDER BY rowid LIMIT 8", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []RewardClaim
	for rows.Next() {
		var claim RewardClaim
		if err = rows.Scan(&claim.Token, &claim.RewardID, &claim.Payload); err != nil {
			return nil, err
		}
		result = append(result, claim)
	}
	return result, rows.Err()
}
