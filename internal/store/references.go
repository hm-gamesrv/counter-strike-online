package store

import "database/sql"

// clearItemReference drops persisted selections that point at a removed item.
// A still-owned copy of the same ID keeps the reference, because selections are
// stored by item ID and another copy can legitimately replace the removed one.
func clearItemReference(tx *sql.Tx, userID, itemID int) error {
	var remaining int
	if err := tx.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=? AND itemID=? AND count>0", userID, itemID).Scan(&remaining); err != nil {
		return err
	}
	if remaining > 0 {
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
	_, err := tx.Exec("DELETE FROM UserZBCostumeLoadout WHERE userID=? AND itemID=?", userID, itemID)
	return err
}
