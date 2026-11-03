package store

import (
	"database/sql"
	"errors"
)

// ClanStoragePage returns the non-empty rows of one storage page. Every clan
// member may view a page; write access is checked separately.
func (s *Store) ClanStoragePage(userID, pageID int) ([]ClanStorageItem, error) {
	rows, err := s.DB.Query("SELECT pageID,slot,itemID,itemCount,itemDuration,itemEnhValue FROM ClanStorageItem WHERE clanID=(SELECT clanID FROM ClanMember WHERE userID=? LIMIT 1) AND pageID=? AND itemID<>0 ORDER BY slot", userID, pageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ClanStorageItem
	for rows.Next() {
		var item ClanStorageItem
		if err = rows.Scan(&item.PageID, &item.Slot, &item.ItemID, &item.Count, &item.Expiry, &item.Enhance); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ClanStorageAccess(userID int) ([]int, error) {
	rows, err := s.DB.Query("SELECT accessGrade FROM ClanStoragePage WHERE clanID=(SELECT clanID FROM ClanMember WHERE userID=? LIMIT 1) ORDER BY pageID", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var grades []int
	for rows.Next() {
		var grade int
		if err = rows.Scan(&grade); err != nil {
			return nil, err
		}
		grades = append(grades, grade)
	}
	return grades, rows.Err()
}

func (s *Store) SetClanStorageAccess(userID, pageID, grade int) (int, error) {
	if pageID < 0 || pageID >= ClanStoragePages || grade < 0 || grade > 3 {
		return -1, nil
	}
	result, err := s.DB.Exec("UPDATE ClanStoragePage SET accessGrade=? WHERE clanID=(SELECT clanID FROM ClanMember WHERE userID=? AND memberGrade<=1 LIMIT 1) AND pageID=?", grade, userID, pageID)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return -1, nil
	}
	return 1, nil
}

func clanStoragePermission(tx *sql.Tx, userID, pageID int) (int, int, error) {
	var clanID, accessGrade, memberGrade int
	err := tx.QueryRow("SELECT m.clanID,p.accessGrade,m.memberGrade FROM ClanMember m INNER JOIN ClanStoragePage p ON p.clanID=m.clanID AND p.pageID=? WHERE m.userID=?", pageID, userID).Scan(&clanID, &accessGrade, &memberGrade)
	return clanID, accessGrade, err
}

// GiveClanItem moves a whole inventory stack into a clan storage slot. Result
// codes: -1 no permission, -2 missing/unsuitable inventory item, -3 page full,
// 0 database error, 1 moved.
func (s *Store) GiveClanItem(userID, pageID, slot int) ([]Item, int, error) {
	if pageID < 0 || pageID >= ClanStoragePages || slot < 1 {
		return nil, -1, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	clanID, accessGrade, err := clanStoragePermission(tx, userID, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, -1, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var memberGrade int
	if err = tx.QueryRow("SELECT memberGrade FROM ClanMember WHERE userID=?", userID).Scan(&memberGrade); err != nil {
		return nil, 0, err
	}
	if memberGrade > accessGrade {
		return nil, -1, nil
	}
	var storageSlot int
	err = tx.QueryRow("SELECT slot FROM ClanStorageItem WHERE clanID=? AND pageID=? AND itemID=0 ORDER BY slot LIMIT 1", clanID, pageID).Scan(&storageSlot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, -3, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var itemID, count, expiry, enhance, clanItem, status int
	err = tx.QueryRow("SELECT itemID,count,coalesce(expiryDate,0),coalesce(enhanceValue,0),coalesce(isClanItem,0),coalesce(status,0) FROM UserInventory WHERE userID=? AND slot=? AND count>0 AND "+liveItemSQL, userID, slot, nowMinutes()).Scan(&itemID, &count, &expiry, &enhance, &clanItem, &status)
	if errors.Is(err, sql.ErrNoRows) || itemID <= 0 || clanItem != 0 {
		return nil, -2, nil
	}
	if err != nil {
		return nil, 0, err
	}
	duration := expiry
	if expiry > 0 && expiry <= 36500 {
		duration = expiry*1440 + nowMinutes()
	}
	if _, err = tx.Exec("UPDATE ClanStorageItem SET itemID=?,itemCount=?,itemDuration=?,itemEnhValue=? WHERE clanID=? AND pageID=? AND slot=?", itemID, count, duration, enhance, clanID, pageID, storageSlot); err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", userID, slot); err != nil {
		return nil, 0, err
	}
	// The deposited copy leaves the bag, so a nameplate, effect, costume or zombie
	// skin pointing at it has to be cleared and a selected weapon needs another
	// copy to take over -- same cascade as disassemble and expiry. Nothing checks
	// whether the item was equipped, so this is reachable from the storage UI.
	if err = clearItemReference(tx, userID, itemID); err != nil {
		return nil, 0, err
	}
	if err = promoteItemSelection(tx, userID, []Item{{
		Slot:   slot,
		ID:     itemID,
		Status: status,
	}}); err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return []Item{{Slot: slot}}, 1, nil
}

// TakeClanItem moves one storage stack into the player's inventory. Result
// codes: -1 no permission or empty slot, -2 inventory full, -3 duplicate
// permanent item, -4 reserved limit, -5 already owns a clan item, 0 database
// error, 1 moved.
func (s *Store) TakeClanItem(userID, pageID, slot, limit, defaultSlots int) ([]Item, int, error) {
	if pageID < 0 || pageID >= ClanStoragePages || slot < 0 || slot >= ClanStoragePageSize || limit < 1 || defaultSlots < 0 || defaultSlots >= 65535 || limit > 65535-defaultSlots {
		return nil, -1, nil
	}
	storageSlot := slot
	now := nowMinutes()
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback()
	clanID, accessGrade, err := clanStoragePermission(tx, userID, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, -1, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var memberGrade int
	if err = tx.QueryRow("SELECT memberGrade FROM ClanMember WHERE userID=?", userID).Scan(&memberGrade); err != nil {
		return nil, 0, err
	}
	if memberGrade > accessGrade {
		return nil, -1, nil
	}
	var itemID, count, duration, enhance int
	err = tx.QueryRow("SELECT itemID,itemCount,itemDuration,itemEnhValue FROM ClanStorageItem WHERE clanID=? AND pageID=? AND slot=? AND itemID<>0", clanID, pageID, slot).Scan(&itemID, &count, &duration, &enhance)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, -1, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var existing int
	if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM UserInventory WHERE userID=? AND itemID=? AND isClanItem=1 LIMIT 1)", userID, itemID).Scan(&existing); err != nil {
		return nil, 0, err
	}
	if existing != 0 {
		return nil, -5, nil
	}
	if duration == 0 {
		if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM UserInventory WHERE userID=? AND itemID=? AND "+liveItemSQL+" LIMIT 1)", userID, itemID, nowMinutes()).Scan(&existing); err != nil {
			return nil, 0, err
		}
		if existing != 0 {
			return nil, -3, nil
		}
	}
	var used, nextSlot int
	if err = tx.QueryRow("SELECT count(*),coalesce(max(slot),0)+1 FROM UserInventory WHERE userID=?", userID).Scan(&used, &nextSlot); err != nil {
		return nil, 0, err
	}
	var configured int
	if err = tx.QueryRow("SELECT nextInventorySlot FROM UserCharacterExtended WHERE userID=?", userID).Scan(&configured); err != nil {
		return nil, 0, err
	}
	slot = max(nextSlot, configured)
	if used >= limit || slot > 65535-defaultSlots {
		return nil, -2, nil
	}
	if _, err = tx.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,?,?,?,1,1,?,?,1,0,0,?,0,0,0,0)", userID, slot, itemID, count, now, duration, enhance); err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec("UPDATE ClanStorageItem SET itemID=0,itemCount=0,itemDuration=0,itemEnhValue=0 WHERE clanID=? AND pageID=? AND slot=?", clanID, pageID, storageSlot); err != nil {
		return nil, 0, err
	}
	if _, err = tx.Exec("UPDATE UserCharacterExtended SET nextInventorySlot=max(nextInventorySlot,?) WHERE userID=?", slot, userID); err != nil {
		return nil, 0, err
	}
	if err = tx.Commit(); err != nil {
		return nil, 0, err
	}
	return []Item{{Slot: slot, ID: itemID, Count: count, Status: 1, InUse: 1, Obtain: now, Expiry: duration, Value: enhance}}, 1, nil
}

func (s *Store) DeleteClanItem(userID, pageID, slot int) (int, error) {
	if pageID < 0 || pageID >= ClanStoragePages || slot < 0 || slot >= ClanStoragePageSize {
		return -1, nil
	}
	result, err := s.DB.Exec("UPDATE ClanStorageItem SET itemID=0,itemCount=0,itemDuration=0,itemEnhValue=0 WHERE clanID=(SELECT clanID FROM ClanMember WHERE userID=? AND memberGrade<=1 LIMIT 1) AND pageID=? AND slot=? AND itemID<>0", userID, pageID, slot)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return -1, nil
	}
	return 1, nil
}
