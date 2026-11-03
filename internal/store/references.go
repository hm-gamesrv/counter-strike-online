package store

import "database/sql"

// clearItemReference drops persisted selections that point at a removed item.
// The reference stays only when another copy of the same ID is still selected
// (inUse) and live; a dormant or expired leftover cannot keep the slot occupied.
func clearItemReference(tx *sql.Tx, userID, itemID int) error {
	var selected int
	err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM UserInventory WHERE userID=? AND itemID=? AND count>0 AND inUse<>0 AND "+liveItemSQL+")", userID, itemID, nowMinutes()).Scan(&selected)
	if err != nil {
		return err
	}
	if selected != 0 {
		return nil
	}
	if _, err := tx.Exec("UPDATE UserCharacter SET nameplateID=0 WHERE userID=? AND nameplateID=?", userID, itemID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE UserCharacterExtended SET zbRespawnEffect=0 WHERE userID=? AND zbRespawnEffect=?", userID, itemID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE UserCharacterExtended SET killerMarkEffect=0 WHERE userID=? AND killerMarkEffect=?", userID, itemID); err != nil {
		return err
	}
	for _, column := range []string{"head", "back", "arm", "pelvis", "face", "tattoo", "pet"} {
		if _, err := tx.Exec("UPDATE UserCostumeLoadout SET "+column+"=0 WHERE userID=? AND "+column+"=?", userID, itemID); err != nil {
			return err
		}
	}
	_, err = tx.Exec("DELETE FROM UserZBCostumeLoadout WHERE userID=? AND itemID=?", userID, itemID)
	return err
}
