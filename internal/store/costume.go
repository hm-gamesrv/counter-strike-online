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

func (s *Store) ToggleCostume(id, slot int, className string) ([]Item, error) {
	field, ok := costumeFields[className]
	if !ok {
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
	loadout := [7]int{}
	err = tx.QueryRow("SELECT head,back,arm,pelvis,face,tattoo,pet FROM UserCostumeLoadout WHERE userID=?", id).Scan(&loadout[0], &loadout[1], &loadout[2], &loadout[3], &loadout[4], &loadout[5], &loadout[6])
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	changed := []Item{}
	newInUse := 1 - target.InUse
	if newInUse == 1 {
		oldID := loadout[field.index]
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
		loadout[field.index] = target.ID
	} else if loadout[field.index] == target.ID {
		loadout[field.index] = 0
	}
	if _, err = tx.Exec("UPDATE UserInventory SET inUse=? WHERE userID=? AND slot=?", newInUse, id, target.Slot); err != nil {
		return nil, err
	}
	target.InUse = newInUse
	changed = append(changed, target)
	result, err := tx.Exec("UPDATE UserCostumeLoadout SET head=?,back=?,arm=?,pelvis=?,face=?,tattoo=?,pet=? WHERE userID=?", loadout[0], loadout[1], loadout[2], loadout[3], loadout[4], loadout[5], loadout[6], id)
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if n == 0 {
		if _, err = tx.Exec("INSERT INTO UserCostumeLoadout(userID,head,back,arm,pelvis,face,tattoo,pet) VALUES(?,?,?,?,?,?,?,?)", id, loadout[0], loadout[1], loadout[2], loadout[3], loadout[4], loadout[5], loadout[6]); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return changed, nil
}
