package store

import "database/sql"

type QuestEvent struct {
	ID, UserID, Kind, Count, Mode, Map, Players int
	Won                                         bool
}

func enqueueQuestEvent(tx *sql.Tx, e QuestEvent) error {
	_, err := tx.Exec("INSERT INTO GoQuestEventOutbox(userID,kind,count,mode,mapID,players,won) VALUES(?,?,?,?,?,?,?)", e.UserID, e.Kind, e.Count, e.Mode, e.Map, e.Players, e.Won)
	return err
}

// Character changes and their events commit together. Delivery may be retried
// after a crash; task-specific event keys make partial delivery idempotent.
func (s *Store) QuestEvents(id int) ([]QuestEvent, error) {
	rows, err := s.DB.Query("SELECT id,userID,kind,count,mode,mapID,players,won FROM GoQuestEventOutbox WHERE userID=? ORDER BY id LIMIT 64", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []QuestEvent
	for rows.Next() {
		var e QuestEvent
		if err = rows.Scan(&e.ID, &e.UserID, &e.Kind, &e.Count, &e.Mode, &e.Map, &e.Players, &e.Won); err != nil {
			return nil, err
		}
		result = append(result, e)
	}
	return result, rows.Err()
}
func (s *Store) CompleteQuestEvent(id, eventID int) error {
	_, err := s.DB.Exec("DELETE FROM GoQuestEventOutbox WHERE userID=? AND id=?", id, eventID)
	return err
}
