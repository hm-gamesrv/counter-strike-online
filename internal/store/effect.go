package store

import (
	"database/sql"
	"errors"
)

// effectColumns are the cosmetic effect choices stored on the extended
// character row. Upstream only persists them and refreshes inventory flags.
var effectColumns = map[string]bool{
	"zbRespawnEffect":  true,
	"killerMarkEffect": true,
}

// activateExpiry returns the expiry to persist when a row is switched on. A
// dormant copy stores a relative day count, which must become an absolute
// timestamp before the item can count as live.
func activateExpiry(tx *sql.Tx, id, slot int) (int, error) {
	var status, expiry int
	err := tx.QueryRow("SELECT coalesce(status,0),coalesce(expiryDate,0) FROM UserInventory WHERE userID=? AND slot=?", id, slot).Scan(&status, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrItemUnavailable
	}
	if err != nil {
		return 0, err
	}
	if status == 0 && expiry > 0 && expiry <= 36500 {
		expiry = nowMinutes() + expiry*1440
	}
	return expiry, nil
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
	expiry, err := activateExpiry(tx, id, slot)
	if err != nil {
		return nil, 0, err
	}
	result, err := tx.Exec("UPDATE UserInventory SET status=1,inUse=?,expiryDate=? WHERE userID=? AND slot=?", inUse, expiry, id, slot)
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
	expiry, err := activateExpiry(tx, id, slot)
	if err != nil {
		return nil, err
	}
	result, err := tx.Exec("UPDATE UserInventory SET status=1,inUse=?,expiryDate=? WHERE userID=? AND slot=?", inUse, expiry, id, slot)
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

// UpdateTournament stores the tournament HUD flag (0 off, 0xFF on); the client
// reads it from the user info block flagged by UFLAG_LOW_TOURNAMENT.
func (s *Store) UpdateTournament(id, value int) (int, error) {
	if _, err := s.DB.Exec("UPDATE UserCharacter SET tournament=? WHERE userID=?", value, id); err != nil {
		return 0, err
	}
	return value, nil
}
