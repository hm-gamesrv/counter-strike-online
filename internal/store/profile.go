package store

func (s *Store) SetChatColor(id, color int) error {
	if color != 0 {
		var owned int
		if err := s.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=? AND itemID=? AND count>0 AND inUse=1 AND (expiryDate=0 OR expiryDate>?)", id, color, nowMinutes()).Scan(&owned); err != nil {
			return err
		}
		if owned == 0 {
			return ErrItemUnavailable
		}
	}
	_, err := s.DB.Exec("UPDATE UserCharacter SET chatColorID=? WHERE userID=?", color, id)
	return err
}

// RenameCharacter validates and applies a nickname item in one transaction.
// Result codes follow upstream: -1 too short, -2 too long, -3 duplicate,
// -4 invalid characters, 8 missing/unusable nickname item, 2 success.
func (s *Store) RenameCharacter(id, slot, itemID int, name string) (int, []Item, error) {
	if len(name) < 4 {
		return -1, nil, nil
	}
	if len(name) > MaxCharacterNameLength {
		return -2, nil, nil
	}
	if !validName.MatchString(name) {
		return -4, nil, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRow("SELECT count(*) FROM UserCharacter WHERE gameName=? COLLATE NOCASE AND userID<>?", name, id).Scan(&exists); err != nil {
		return 0, nil, err
	}
	if exists != 0 {
		return -3, nil, nil
	}
	items, err := readInventory(tx, id)
	if err != nil {
		return 0, nil, err
	}
	var source Item
	for _, item := range items {
		if item.Slot == slot {
			source = item
			break
		}
	}
	if source.ID != itemID || source.Count < 1 || source.Status != 0 {
		return 8, nil, nil
	}
	removed := source.Count == 1
	if removed {
		_, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, slot)
	} else {
		_, err = tx.Exec("UPDATE UserInventory SET count=count-1 WHERE userID=? AND slot=?", id, slot)
	}
	if err != nil {
		return 0, nil, err
	}
	if _, err = tx.Exec("UPDATE UserCharacter SET gameName=? WHERE userID=?", name, id); err != nil {
		return 0, nil, err
	}
	items, err = readInventory(tx, id)
	if err != nil {
		return 0, nil, err
	}
	if removed {
		items = append([]Item{{Slot: slot}}, items...)
	}
	if err = tx.Commit(); err != nil {
		return 0, nil, err
	}
	return 2, items, nil
}
