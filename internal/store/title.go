package store

import (
	"errors"
	"strconv"
	"strings"
)

// TitleSlots is the number of title IDs the client can cycle through.
const TitleSlots = 5

var ErrTitleSlot = errors.New("invalid title slot")

func parseIntList(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	parts := strings.Split(raw, ",")
	values := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func serializeTitles(titles [TitleSlots]int) string {
	parts := make([]string, TitleSlots)
	for i, title := range titles {
		parts[i] = strconv.Itoa(title)
	}
	return strings.Join(parts, ",")
}

// SetPrefix changes the title shown in front of the player name.
func (s *Store) SetPrefix(id, prefix int) error {
	_, err := s.DB.Exec("UPDATE UserCharacter SET prefixID=? WHERE userID=?", prefix, id)
	return err
}

// SetTitle replaces one slot of the rotating title list. A zero title clears
// the slot, matching the upstream list-remove request.
func (s *Store) SetTitle(id, slot, title int) error {
	if slot < 0 || slot >= TitleSlots {
		return ErrTitleSlot
	}
	// The column holds all slots in one string, so this is a read-modify-write.
	// Two autocommit statements would let a second writer (a reconnect taking
	// over the account, or a concurrent GM command) overwrite the slot this
	// call just wrote; the transaction keeps the whole update atomic.
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var serialized string
	if err = tx.QueryRow("SELECT titles FROM UserCharacter WHERE userID=?", id).Scan(&serialized); err != nil {
		return err
	}
	values, err := parseIntList(serialized)
	if err != nil {
		return err
	}
	var titles [TitleSlots]int
	copy(titles[:], values)
	titles[slot] = title
	if _, err = tx.Exec("UPDATE UserCharacter SET titles=? WHERE userID=?", serializeTitles(titles), id); err != nil {
		return err
	}
	return tx.Commit()
}
