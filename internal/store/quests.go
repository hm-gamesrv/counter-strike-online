package store

import (
	"database/sql"
	"errors"
)

type QuestProgress struct{ Units, Goal, Token int }

func (s *Store) QuestProgress(id, quest, task int) (QuestProgress, error) {
	var p QuestProgress
	err := s.DB.QueryRow("SELECT units,goal,token FROM GoQuestTaskProgress WHERE userID=? AND questID=? AND taskID=?", id, quest, task).Scan(&p.Units, &p.Goal, &p.Token)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return p, err
}

// RecordQuestProgress caps cumulative progress and freezes a completion reward
// in the same transaction. A task's permanent token prevents awarding it twice,
// even when its notice fails, inventory is full, or the server restarts.
func (s *Store) RecordQuestProgress(id, quest, task, goal, delta, rewardID int, payload []byte) (QuestProgress, error) {
	return s.RecordQuestProgressOnce(id, quest, task, goal, delta, rewardID, payload, "")
}

func (s *Store) RecordQuestProgressOnce(id, quest, task, goal, delta, rewardID int, payload []byte, eventKey string) (QuestProgress, error) {
	var p QuestProgress
	if id < 1 || quest < 1 || task < 1 || goal < 1 || goal > 1000000000 || delta < 0 || delta > 1000000000 || rewardID < 0 || len(payload) > 1048576 || len(payload) > 0 && rewardID == 0 || len(eventKey) > 200 {
		return p, errors.New("invalid quest progress")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("INSERT OR IGNORE INTO GoQuestTaskProgress(userID,questID,taskID,goal) VALUES(?,?,?,?)", id, quest, task, goal); err != nil {
		return p, err
	}
	var rowID int
	if err = tx.QueryRow("SELECT rowid,units,goal,token FROM GoQuestTaskProgress WHERE userID=? AND questID=? AND taskID=?", id, quest, task).Scan(&rowID, &p.Units, &p.Goal, &p.Token); err != nil {
		return p, err
	}
	if p.Goal != goal {
		return p, errors.New("quest goal changed; use a new task ID")
	}
	if p.Units < 0 || p.Units > p.Goal {
		return p, errors.New("invalid stored quest progress")
	}
	if eventKey != "" {
		result, err := tx.Exec("INSERT OR IGNORE INTO GoQuestEvent VALUES(?,?,?,?)", id, quest, task, eventKey)
		if err != nil {
			return p, err
		}
		if n, err := result.RowsAffected(); err != nil {
			return p, err
		} else if n == 0 {
			return p, tx.Commit()
		}
	}
	p.Units += min(delta, p.Goal-p.Units)
	if p.Units == p.Goal && p.Token == 0 && len(payload) > 0 {
		if p.Token, err = newRewardToken(tx, id); err != nil {
			return p, err
		}
		// Negative source slots identify server tasks, never inventory entries.
		if _, err = tx.Exec("INSERT INTO GoRewardClaim(userID,token,rewardID,sourceSlot,payload) VALUES(?,?,?,?,?)", id, p.Token, rewardID, -rowID, payload); err != nil {
			return p, err
		}
	}
	if _, err = tx.Exec("UPDATE GoQuestTaskProgress SET units=?,token=? WHERE userID=? AND questID=? AND taskID=?", p.Units, p.Token, id, quest, task); err != nil {
		return p, err
	}
	return p, tx.Commit()
}
