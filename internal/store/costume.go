package store

import (
	"database/sql"
	"errors"
)

type costumeField struct {
	index  int
	column string
}

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
	if !regular && !zombie || zombie && (zombieSkin <= 0 || zombieSkin > 255) {
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
	} else if err != nil {
		return nil, err
	}
	changed := []Item{}
	newInUse := 1 - target.InUse
	if newInUse == 1 {
		oldID := loadout[field]
		if oldID != 0 && oldID != target.ID {
			for _, item := range items {
				if item.ID == oldID && item.InUse == 1 {
					if _, err = tx.Exec("UPDATE UserInventory SET inUse=0 WHERE userID=? AND slot=?", id, item.Slot); err != nil {
						return nil, err
					}
					item.InUse = 0
					changed = append(changed, item)
					break
				}
			}
		}
		loadout[field] = target.ID
	} else if loadout[field] == target.ID {
		loadout[field] = 0
	}
	if _, err = tx.Exec("UPDATE UserInventory SET inUse=? WHERE userID=? AND slot=?", newInUse, id, target.Slot); err != nil {
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
	} else if err != nil {
		return nil, err
	}
	changed := []Item{}
	newInUse := 1 - target.InUse
	if newInUse == 1 {
		if exists && current != 0 && current != target.ID {
			for _, item := range items {
				if item.ID == current && item.InUse == 1 {
					if _, err = tx.Exec("UPDATE UserInventory SET inUse=0 WHERE userID=? AND slot=?", id, item.Slot); err != nil {
						return nil, err
					}
					item.InUse = 0
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
	if _, err = tx.Exec("UPDATE UserInventory SET inUse=? WHERE userID=? AND slot=?", newInUse, id, target.Slot); err != nil {
		return nil, err
	}
	target.InUse = newInUse
	return append(changed, target), nil
}
