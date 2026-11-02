package store

import "errors"

// effectColumns are the cosmetic effect choices stored on the extended
// character row. Upstream only persists them and refreshes inventory flags.
var effectColumns = map[string]bool{
	"zbRespawnEffect":  true,
	"killerMarkEffect": true,
}

// ToggleNameplate switches a LobbyBG nameplate, mirroring the nameplate branch
// of CItemManager::OnItemPacket. It returns the inventory rows it touched and
// the new UserCharacter.nameplateID.
func (s *Store) ToggleNameplate(id, itemID, slot int) ([]Item, int, error) {
	if slot <= 0 {
		return nil, 0, errors.New("invalid nameplate toggle")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	var current int
	if err = tx.QueryRow("SELECT coalesce(nameplateID,0) FROM UserCharacter WHERE userID=?", id).Scan(&current); err != nil {
		return nil, 0, err
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
		return nil, 0, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return nil, 0, err
	} else if n == 0 {
		return nil, 0, ErrItemUnavailable
	}
	if _, err = tx.Exec("UPDATE UserInventory SET status=0 WHERE userID=? AND itemID=? AND slot<>? AND status<>0", id, itemID, slot); err != nil {
		return nil, 0, err
	}
	if current != 0 && current != itemID {
		if _, err = tx.Exec("UPDATE UserInventory SET inUse=0 WHERE userID=? AND itemID=? AND inUse<>0", id, current); err != nil {
			return nil, 0, err
		}
	}
	if _, err = tx.Exec("UPDATE UserCharacter SET nameplateID=? WHERE userID=?", next, id); err != nil {
		return nil, 0, err
	}
	items, err := readInventory(tx, id)
	if err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	changed := make([]Item, 0, len(items))
	for _, item := range items {
		if item.ID == itemID || item.ID == current {
			changed = append(changed, item)
		}
	}
	return changed, next, nil
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
