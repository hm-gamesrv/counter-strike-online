package store

import (
	"errors"
	"math"
	"time"
)

type MatchReward struct {
	UserID, Exp, Points, Kills, Deaths int
	UpdateStats                        bool
	QuestMatchEnd, Won                 bool
	Mode, Map, Players                 int
}

// SettleMatch commits all participants together. The durable match/user key
// prevents retrying a failed response from paying the same match twice.
func (s *Store) SettleMatch(matchID string, rewards []MatchReward) error {
	if matchID == "" {
		return errors.New("missing match ID")
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`CREATE TABLE IF NOT EXISTS GoMatchSettlement(matchID TEXT NOT NULL,userID INT NOT NULL,exp INT NOT NULL,points INT NOT NULL,settledAt INT NOT NULL,PRIMARY KEY(matchID,userID))`); e != nil {
		return e
	}
	for _, reward := range rewards {
		if reward.UserID <= 0 || reward.Exp < 0 || reward.Exp > 10000000 || reward.Points < 0 || reward.Points > 10000000 || reward.Kills < 0 || uint64(reward.Kills) > math.MaxUint32 || reward.Deaths < 0 || uint64(reward.Deaths) > math.MaxUint32 {
			return errors.New("invalid match reward")
		}
		result, e := tx.Exec("INSERT OR IGNORE INTO GoMatchSettlement VALUES(?,?,?,?,?)", matchID, reward.UserID, reward.Exp, reward.Points, time.Now().Unix())
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n == 0 {
			continue
		}
		var exp, level int
		if e = tx.QueryRow("SELECT exp,level FROM UserCharacter WHERE userID=?", reward.UserID).Scan(&exp, &level); e != nil {
			return e
		}
		oldLevel := level
		// Retain the upstream public build's experience cap and level thresholds.
		if exp < 0 {
			return errors.New("negative character experience")
		}
		exp = min(exp, 30069814)
		exp += min(reward.Exp, 30069814-exp)
		newLevel := levelForExp(exp)
		level = max(level, newLevel)
		kills, deaths := 0, 0
		if reward.UpdateStats {
			kills = reward.Kills
			deaths = reward.Deaths
		}
		result, e = tx.Exec("UPDATE UserCharacter SET exp=?,level=?,points=points+?,kills=kills+?,deaths=deaths+? WHERE userID=? AND points BETWEEN 0 AND ? AND kills BETWEEN 0 AND ? AND deaths BETWEEN 0 AND ?", exp, level, reward.Points, kills, deaths, reward.UserID, int64(math.MaxInt64)-int64(reward.Points), int64(math.MaxUint32)-int64(kills), int64(math.MaxUint32)-int64(deaths))
		if e != nil {
			return e
		}
		if n, e = result.RowsAffected(); e != nil {
			return e
		} else if n != 1 {
			return errors.New("match reward exceeds character counter range")
		}
		if level > oldLevel {
			if e = enqueueQuestEvent(tx, QuestEvent{UserID: reward.UserID, Kind: 5, Count: level - oldLevel}); e != nil {
				return e
			}
		}
		if reward.QuestMatchEnd {
			if reward.Mode < 0 || reward.Mode > 255 || reward.Map < 0 || reward.Map > 65535 || reward.Players < 1 || reward.Players > 32 {
				return errors.New("invalid quest match context")
			}
			if e = enqueueQuestEvent(tx, QuestEvent{UserID: reward.UserID, Kind: 1, Count: 1, Mode: reward.Mode, Map: reward.Map, Players: reward.Players, Won: reward.Won}); e != nil {
				return e
			}
		}
	}
	return tx.Commit()
}
