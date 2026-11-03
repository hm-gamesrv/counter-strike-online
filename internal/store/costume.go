package store

import (
	"database/sql"
	"errors"
)

type costumeField struct {
	index  int
	column string
}

// zbCostumeSlots mirrors ZB_COSTUME_SLOT_COUNT_MAX from the reference: zombie
// skin costumes are keyed by the ZombieSkin column, which starts at zero.
const zbCostumeSlots = 18

var costumeFields = map[string]costumeField{
	"HeadCostume":   {0, "head"},
	"BackCostume":   {1, "back"},
	"ArmCostume":    {2, "arm"},
	"PelvisCostume": {3, "pelvis"},
	"FaceCostume":   {4, "face"},
	"Tattoo":        {5, "tattoo"},
	"PetCostume":    {6, "pet"},
}

func (s *Store) ToggleCostume(id, slot int, className string, zombieSkin int) ([]Item, error) {
	field, regular := costumeFields[className]
	zombie := className == "ZombieSkinCostume"
	if !regular && !zombie || zombie && (zombieSkin < 0 || zombieSkin >= zbCostumeSlots) {
		return nil, ErrItemUnavailable
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, err := readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	var target Item
	for _, item := range items {
		if item.Slot == slot {
			target = item
			break
		}
	}
	if target.ID == 0 {
		return nil, ErrItemUnavailable
	}
	var changed []Item
	if zombie {
		changed, err = toggleZombieSkinCostume(tx, id, items, target, zombieSkin)
	} else {
		changed, err = toggleRegularCostume(tx, id, items, target, field.index)
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return changed, nil
}

func toggleRegularCostume(tx *sql.Tx, id int, items []Item, target Item, field int) ([]Item, error) {
	loadout := [7]int{}
	exists := true
	err := tx.QueryRow("SELECT head,back,arm,pelvis,face,tattoo,pet FROM UserCostumeLoadout WHERE userID=?", id).Scan(&loadout[0], &loadout[1], &loadout[2], &loadout[3], &loadout[4], &loadout[5], &loadout[6])
	if errors.Is(err, sql.ErrNoRows) {
		exists = false
		err = nil
	} else if err != nil {
		return nil, err
	}
	changed := []Item{}
	newInUse := 1 - target.InUse
	if newInUse == 1 {
		oldID := loadout[field]
		if oldID != 0 {
			// Replace the previously equipped copy even when it carries the same
			// item ID: two copies of one costume must never stay equipped together.
			for _, item := range items {
				if item.ID == oldID && item.InUse == 1 && item.Slot != target.Slot {
					// The reference keeps the replaced costume active (status 1)
					// and only clears its inUse flag.
					if _, err = tx.Exec("UPDATE UserInventory SET inUse=0,status=1 WHERE userID=? AND slot=?", id, item.Slot); err != nil {
						return nil, err
					}
					item.InUse = 0
					item.Status = 1
					changed = append(changed, item)
					break
				}
			}
		}
		loadout[field] = target.ID
	} else if loadout[field] == target.ID {
		loadout[field] = 0
	}
	if newInUse == 1 {
		// Equipping activates a dormant copy: the reference converts the stored
		// relative duration into an absolute expiry date and sets status 1.
		if target.Status == 0 {
			if target.Expiry > 0 {
				target.Expiry = nowMinutes() + target.Expiry*1440
			}
			target.Status = 1
		}
		if _, err = tx.Exec("UPDATE UserInventory SET inUse=1,status=?,expiryDate=? WHERE userID=? AND slot=?", target.Status, target.Expiry, id, target.Slot); err != nil {
			return nil, err
		}
	} else if _, err = tx.Exec("UPDATE UserInventory SET inUse=0 WHERE userID=? AND slot=?", id, target.Slot); err != nil {
		return nil, err
	}
	target.InUse = newInUse
	changed = append(changed, target)
	if exists {
		_, err = tx.Exec("UPDATE UserCostumeLoadout SET head=?,back=?,arm=?,pelvis=?,face=?,tattoo=?,pet=? WHERE userID=?", loadout[0], loadout[1], loadout[2], loadout[3], loadout[4], loadout[5], loadout[6], id)
	} else {
		_, err = tx.Exec("INSERT INTO UserCostumeLoadout(userID,head,back,arm,pelvis,face,tattoo,pet) VALUES(?,?,?,?,?,?,?,?)", id, loadout[0], loadout[1], loadout[2], loadout[3], loadout[4], loadout[5], loadout[6])
	}
	if err != nil {
		return nil, err
	}
	return changed, nil
}

// Zombie skins are stored per skin type in UserZBCostumeLoadout, so several
// zombie classes can keep an independently equipped costume.
func toggleZombieSkinCostume(tx *sql.Tx, id int, items []Item, target Item, skin int) ([]Item, error) {
	var current int
	err := tx.QueryRow("SELECT itemID FROM UserZBCostumeLoadout WHERE userID=? AND slot=?", id, skin).Scan(&current)
	exists := true
	if errors.Is(err, sql.ErrNoRows) {
		exists = false
		err = nil
	} else if err != nil {
		return nil, err
	}
	changed := []Item{}
	newInUse := 1 - target.InUse
	if newInUse == 1 {
		if exists && current != 0 {
			for _, item := range items {
				if item.ID == current && item.InUse == 1 && item.Slot != target.Slot {
					if _, err = tx.Exec("UPDATE UserInventory SET inUse=0,status=1 WHERE userID=? AND slot=?", id, item.Slot); err != nil {
						return nil, err
					}
					item.InUse = 0
					item.Status = 1
					changed = append(changed, item)
					break
				}
			}
		}
		if exists {
			_, err = tx.Exec("UPDATE UserZBCostumeLoadout SET itemID=? WHERE userID=? AND slot=?", target.ID, id, skin)
		} else {
			_, err = tx.Exec("INSERT INTO UserZBCostumeLoadout(userID,slot,itemID) VALUES(?,?,?)", id, skin, target.ID)
		}
	} else if exists && current == target.ID {
		_, err = tx.Exec("DELETE FROM UserZBCostumeLoadout WHERE userID=? AND slot=?", id, skin)
	}
	if err != nil {
		return nil, err
	}
	if newInUse == 1 {
		if target.Status == 0 {
			if target.Expiry > 0 {
				target.Expiry = nowMinutes() + target.Expiry*1440
			}
			target.Status = 1
		}
		if _, err = tx.Exec("UPDATE UserInventory SET inUse=1,status=?,expiryDate=? WHERE userID=? AND slot=?", target.Status, target.Expiry, id, target.Slot); err != nil {
			return nil, err
		}
	} else if _, err = tx.Exec("UPDATE UserInventory SET inUse=0 WHERE userID=? AND slot=?", id, target.Slot); err != nil {
		return nil, err
	}
	target.InUse = newInUse
	return append(changed, target), nil
}
func IsCostumeClass(className string) bool {
	_, ok := costumeFields[className]
	return ok || className == "ZombieSkinCostume"
}

// AutoEquipCostume equips a newly granted costume when its loadout slot is
// still empty, matching the upstream AddItem behaviour.
func (s *Store) AutoEquipCostume(id, itemID int, className string, zombieSkin int) (bool, error) {
	field, regular := costumeFields[className]
	zombie := className == "ZombieSkinCostume"
	if !regular && !zombie || zombie && (zombieSkin < 0 || zombieSkin >= zbCostumeSlots) {
		return false, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var slot, status, expiry int
	// A dormant copy keeps its duration as a relative day count, so it is not
	// "expired"; the absolute check only rejects items that already ran out.
	err = tx.QueryRow("SELECT slot,status,expiryDate FROM UserInventory WHERE userID=? AND itemID=? AND inUse=0 AND status=0 AND (expiryDate=0 OR expiryDate>? OR expiryDate BETWEEN 1 AND 36500) ORDER BY slot LIMIT 1", id, itemID, nowMinutes()).Scan(&slot, &status, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if regular {
		loadout := [7]int{}
		exists := true
		err = tx.QueryRow("SELECT head,back,arm,pelvis,face,tattoo,pet FROM UserCostumeLoadout WHERE userID=?", id).Scan(&loadout[0], &loadout[1], &loadout[2], &loadout[3], &loadout[4], &loadout[5], &loadout[6])
		if errors.Is(err, sql.ErrNoRows) {
			exists = false
		} else if err != nil {
			return false, err
		}
		if loadout[field.index] != 0 {
			// A loadout entry can outlive its item (expiry, disassemble). Only a
			// live copy keeps the slot occupied.
			var live int
			if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM UserInventory WHERE userID=? AND itemID=? AND count>0 AND inUse<>0 AND "+liveItemSQL+")", id, loadout[field.index], nowMinutes()).Scan(&live); err != nil {
				return false, err
			}
			if live != 0 {
				return false, nil
			}
			loadout[field.index] = 0
		}
		loadout[field.index] = itemID
		if exists {
			_, err = tx.Exec("UPDATE UserCostumeLoadout SET head=?,back=?,arm=?,pelvis=?,face=?,tattoo=?,pet=? WHERE userID=?", loadout[0], loadout[1], loadout[2], loadout[3], loadout[4], loadout[5], loadout[6], id)
		} else {
			_, err = tx.Exec("INSERT INTO UserCostumeLoadout(userID,head,back,arm,pelvis,face,tattoo,pet) VALUES(?,?,?,?,?,?,?,?)", id, loadout[0], loadout[1], loadout[2], loadout[3], loadout[4], loadout[5], loadout[6])
		}
	} else {
		var existing int
		err = tx.QueryRow("SELECT itemID FROM UserZBCostumeLoadout WHERE userID=? AND slot=?", id, zombieSkin).Scan(&existing)
		if err == nil {
			var live int
			if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM UserInventory WHERE userID=? AND itemID=? AND count>0 AND inUse<>0 AND "+liveItemSQL+")", id, existing, nowMinutes()).Scan(&live); err != nil {
				return false, err
			}
			if live != 0 {
				return false, nil
			}
			if _, err = tx.Exec("DELETE FROM UserZBCostumeLoadout WHERE userID=? AND slot=?", id, zombieSkin); err != nil {
				return false, err
			}
			err = sql.ErrNoRows
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return false, err
		}
		_, err = tx.Exec("INSERT INTO UserZBCostumeLoadout(userID,slot,itemID) VALUES(?,?,?)", id, zombieSkin, itemID)
	}
	if err != nil {
		return false, err
	}
	if status == 0 {
		if expiry > 0 {
			expiry = nowMinutes() + expiry*1440
		}
		status = 1
	}
	if _, err = tx.Exec("UPDATE UserInventory SET inUse=1,status=?,expiryDate=? WHERE userID=? AND slot=?", status, expiry, id, slot); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
