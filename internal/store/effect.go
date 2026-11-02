package store

import "errors"

// effectColumns are the cosmetic effect choices stored on the extended
// character row. Upstream only persists them and refreshes inventory flags.
var effectColumns = map[string]bool{
	"zbRespawnEffect":  true,
	"killerMarkEffect": true,
}

// ToggleEffectItem switches a cosmetic effect item on or off. Activating one
// clears the previously active effect and the status flags of other copies of
// the same item, mirroring CItemManager::OnItemPacket.
func (s *Store) ToggleEffectItem(id, itemID, slot int, column string) ([]Item, error) {
	if !effectColumns[column] || slot <= 0 {
		return nil, errors.New("invalid effect toggle")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var current int
	if err = tx.QueryRow("SELECT coalesce("+column+",0) FROM UserCharacterExtended WHERE userID=?", id).Scan(&current); err != nil {
		return nil, err
	}
	activate := current != itemID
	next := 0
	inUse := 0
	if activate {
		next = itemID
		inUse = 1
	}
	result, err := tx.Exec("UPDATE UserInventory SET status=1,inUse=? WHERE userID=? AND slot=?", inUse, id, slot)
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		return nil, ErrItemUnavailable
	}
	if _, err = tx.Exec("UPDATE UserInventory SET status=0 WHERE userID=? AND itemID=? AND slot<>? AND status<>0", id, itemID, slot); err != nil {
		return nil, err
	}
	if current != 0 && current != itemID {
		if _, err = tx.Exec("UPDATE UserInventory SET inUse=0 WHERE userID=? AND itemID=? AND inUse<>0", id, current); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec("UPDATE UserCharacterExtended SET "+column+"=? WHERE userID=?", next, id); err != nil {
		return nil, err
	}
	items, err := readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	changed := make([]Item, 0, len(items))
	for _, item := range items {
		if item.ID == itemID || item.ID == current {
			changed = append(changed, item)
		}
	}
	return changed, nil
}
