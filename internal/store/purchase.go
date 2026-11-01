package store

import (
	"database/sql"
	"errors"
	"math"
)

type Reward struct {
	ID, Count, Duration int
	// Dormant items await activation/equipping; countables are available but
	// unselected. Exclusive weapons replace the selected copy of the same ID.
	Dormant, Unselected, Exclusive, Stackable bool
}

// Purchase atomically charges the account and grants every bundle item. A full
// inventory or failed insert cannot charge a user or partially grant a bundle.
func (s *Store) Purchase(id int, points bool, price, bonus int, items []Reward, limit, defaultSlots int) error {
	if price < 0 || bonus < 0 || len(items) == 0 {
		return errors.New("BUY_FAIL_SYSTEM_ERROR")
	}
	if err := validateGrants(items, limit, defaultSlots); err != nil {
		return err
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = chargePurchase(tx, id, points, price, bonus); e != nil {
		return e
	}
	if e = grantItems(tx, id, items, limit, defaultSlots); e != nil {
		return e
	}
	return tx.Commit()
}

func validateGrants(items []Reward, limit, defaultSlots int) error {
	if limit < 1 || defaultSlots < 0 || defaultSlots >= 65535 || limit > 65535-defaultSlots {
		return errors.New("BUY_FAIL_SYSTEM_ERROR")
	}
	for _, i := range items {
		if i.ID <= 0 || i.ID > 65535 || i.Count <= 0 || i.Count > 65535 || i.Duration < 0 || i.Duration > 36500 {
			return errors.New("BUY_FAIL_SYSTEM_ERROR")
		}
	}
	return nil
}

func chargePurchase(tx *sql.Tx, id int, points bool, price, bonus int) error {
	column := "cash"
	if points {
		column = "points"
	}
	result, e := tx.Exec("UPDATE UserCharacter SET "+column+"="+column+"-? WHERE userID=? AND "+column+">=?", price, id, price)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return errors.New("BUY_FAIL_NO_POINT")
	}
	result, e = tx.Exec("UPDATE UserCharacter SET points=points+? WHERE userID=? AND points BETWEEN 0 AND ?", bonus, id, int64(math.MaxInt64)-int64(bonus))
	if e != nil {
		return e
	}
	if n, e = result.RowsAffected(); e != nil {
		return e
	} else if n != 1 {
		return errors.New("BUY_FAIL_SYSTEM_ERROR")
	}
	return nil
}

// grantItems is shared by purchases, consumable rewards and future quest claims.
// The caller owns the transaction so source consumption and awards commit together.
func grantItems(tx *sql.Tx, id int, items []Reward, limit, defaultSlots int) error {
	var count, slot int
	if err := tx.QueryRow("SELECT count(*),coalesce(max(slot),0)+1 FROM UserInventory WHERE userID=?", id).Scan(&count, &slot); err != nil {
		return err
	}
	var nextSlot int
	if err := tx.QueryRow("SELECT nextInventorySlot FROM UserCharacterExtended WHERE userID=?", id).Scan(&nextSlot); err != nil {
		return err
	}
	slot = max(slot, nextSlot)
	now := nowMinutes()
	var e error
	for _, i := range items {
		if i.ID == 166 { // Password boxes are a character counter upstream.
			result, err := tx.Exec("UPDATE UserCharacter SET passwordBoxes=passwordBoxes+? WHERE userID=? AND passwordBoxes+?<=65535", i.Count, id, i.Count)
			if err != nil {
				return err
			}
			if affected, err := result.RowsAffected(); err != nil || affected != 1 {
				if err != nil {
					return err
				}
				return errors.New("BUY_FAIL_SYSTEM_ERROR")
			}
			continue
		}
		if i.Stackable {
			var existingSlot, existingCount int
			err := tx.QueryRow("SELECT slot,count FROM UserInventory WHERE userID=? AND itemID=? AND inUse=1 AND (expiryDate=0 OR expiryDate>?) ORDER BY slot LIMIT 1", id, i.ID, now).Scan(&existingSlot, &existingCount)
			if err == nil {
				if existingCount < 1 || existingCount > 65535-i.Count {
					return errors.New("BUY_FAIL_SYSTEM_ERROR")
				}
				if _, e = tx.Exec("UPDATE UserInventory SET count=count+? WHERE userID=? AND slot=?", i.Count, id, existingSlot); e != nil {
					return e
				}
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if count >= limit || slot > 65535-defaultSlots {
			return errors.New("BUY_FAIL_INVENTORY_FULL")
		}
		expiry := 0
		if i.Duration > 0 {
			expiry = now + i.Duration*1440
		}
		status, inUse := 1, 1
		if i.Unselected {
			status = 0
		}
		if i.Dormant {
			status, inUse = 0, 0
			expiry = i.Duration
		}
		if i.Exclusive && status != 0 {
			if _, e = tx.Exec("UPDATE UserInventory SET status=0 WHERE userID=? AND itemID=?", id, i.ID); e != nil {
				return e
			}
		}
		_, e = tx.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,?,?,?,?,?,?,?,0,0,0,0,0,0,0,0)", id, slot, i.ID, i.Count, status, inUse, now, expiry)
		if e != nil {
			return e
		}
		slot++
		count++
	}
	if _, e = tx.Exec("UPDATE UserCharacterExtended SET nextInventorySlot=max(nextInventorySlot,?) WHERE userID=?", slot, id); e != nil {
		return e
	}
	return nil
}
