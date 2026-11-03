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

// Purchase is PurchaseTracked without the affected rows.
func (s *Store) Purchase(id int, points bool, price, bonus int, items []Reward, limit, defaultSlots int) error {
	_, err := s.PurchaseTracked(id, points, price, bonus, items, limit, defaultSlots)
	return err
}

// PurchaseTracked atomically charges the account and grants every bundle item,
// and reports the rows it added or incremented so the caller can send exactly
// that patch. A full inventory or failed insert cannot charge a user or
// partially grant a bundle.
func (s *Store) PurchaseTracked(id int, points bool, price, bonus int, items []Reward, limit, defaultSlots int) ([]Item, error) {
	if price < 0 || bonus < 0 || len(items) == 0 {
		return nil, errors.New("BUY_FAIL_SYSTEM_ERROR")
	}
	if err := validateGrants(items, limit, defaultSlots); err != nil {
		return nil, err
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = chargePurchase(tx, id, points, price, bonus); e != nil {
		return nil, e
	}
	touched, e := grantItemsForPurchase(tx, id, items, limit, defaultSlots, true)
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return touched, nil
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
	_, err := grantItemsTracked(tx, id, items, limit, defaultSlots)
	return err
}

// grantItemsTracked is grantItems plus the rows it touched: a merged stack, or
// the row that was just inserted. Password boxes are a character counter and are
// not reported.
func grantItemsTracked(tx *sql.Tx, id int, items []Reward, limit, defaultSlots int) ([]Item, error) {
	return grantItemsForPurchase(tx, id, items, limit, defaultSlots, false)
}

// grantItemsForPurchase is grantItemsTracked with the AddItem rule that buying a
// timed item the account already owns extends that copy instead of opening a
// second row. It is limited to the purchase path: the reward and prize paths keep
// inserting, which is where their fixtures and the reference's own reward flow
// differ from a shop transaction.
func grantItemsForPurchase(tx *sql.Tx, id int, items []Reward, limit, defaultSlots int, extendTimed bool) ([]Item, error) {
	var touched []Item
	var count, slot int
	if err := tx.QueryRow("SELECT count(*),coalesce(max(slot),0)+1 FROM UserInventory WHERE userID=?", id).Scan(&count, &slot); err != nil {
		return nil, err
	}
	var nextSlot int
	if err := tx.QueryRow("SELECT nextInventorySlot FROM UserCharacterExtended WHERE userID=?", id).Scan(&nextSlot); err != nil {
		return nil, err
	}
	slot = max(slot, nextSlot)
	now := nowMinutes()
	var e error
	for _, i := range items {
		if i.ID == 166 { // Password boxes are a character counter upstream.
			result, err := tx.Exec("UPDATE UserCharacter SET passwordBoxes=passwordBoxes+? WHERE userID=? AND passwordBoxes+?<=65535", i.Count, id, i.Count)
			if err != nil {
				return nil, err
			}
			if affected, err := result.RowsAffected(); err != nil || affected != 1 {
				if err != nil {
					return nil, err
				}
				return nil, errors.New("BUY_FAIL_SYSTEM_ERROR")
			}
			continue
		}
		if i.Stackable {
			var existingSlot, existingCount int
			err := tx.QueryRow("SELECT slot,count FROM UserInventory WHERE userID=? AND itemID=? AND inUse=1 AND (expiryDate=0 OR expiryDate>?) ORDER BY slot LIMIT 1", id, i.ID, now).Scan(&existingSlot, &existingCount)
			if err == nil {
				if existingCount < 1 || existingCount > 65535-i.Count {
					return nil, errors.New("BUY_FAIL_SYSTEM_ERROR")
				}
				if _, e = tx.Exec("UPDATE UserInventory SET count=count+? WHERE userID=? AND slot=?", i.Count, id, existingSlot); e != nil {
					return nil, e
				}
				touched = append(touched, Item{
					Slot:  existingSlot,
					ID:    i.ID,
					Count: existingCount + i.Count,
				})
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
		if extendTimed && i.Duration > 0 {
			// CItemManager::AddItem extends a timed item the account already owns
			// instead of adding a second row, using ExtendItem with the day count as
			// the increment. A copy that is not in use gets the raw day count added
			// (the reference adds days onto an absolute timestamp there), an in-use
			// copy gets days*1440 minutes, and a copy whose total would exceed 999
			// days becomes permanent.
			var extendSlot, extendExpiry, extendObtain, extendInUse int
			err := tx.QueryRow("SELECT slot,coalesce(expiryDate,0),coalesce(obtainDate,0),coalesce(inUse,0) FROM UserInventory WHERE userID=? AND itemID=? AND expiryDate<>0 AND enhanceValue=0 AND partSlot1=0 AND partSlot2=0 LIMIT 1", id, i.ID).Scan(&extendSlot, &extendExpiry, &extendObtain, &extendInUse)
			if err == nil {
				expiry := extendExpiry
				switch {
				case extendInUse == 0:
					expiry += i.Duration
				case i.Duration+(expiry-extendObtain)/1440 >= 999:
					expiry = 0
				default:
					expiry += i.Duration * 1440
				}
				if _, e = tx.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=? AND slot=?", expiry, id, extendSlot); e != nil {
					return nil, e
				}
				touched = append(touched, Item{
					Slot:   extendSlot,
					ID:     i.ID,
					Count:  1,
					Obtain: extendObtain,
					Expiry: expiry,
					InUse:  extendInUse,
				})
				continue
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
		}
		if count >= limit || slot > 65535-defaultSlots {
			return nil, errors.New("BUY_FAIL_INVENTORY_FULL")
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
				return nil, e
			}
		}
		_, e = tx.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,?,?,?,?,?,?,?,0,0,0,0,0,0,0,0)", id, slot, i.ID, i.Count, status, inUse, now, expiry)
		if e != nil {
			return nil, e
		}
		touched = append(touched, Item{
			Slot:   slot,
			ID:     i.ID,
			Count:  i.Count,
			Status: status,
			InUse:  inUse,
			Obtain: now,
			Expiry: expiry,
		})
		slot++
		count++
	}
	if _, e = tx.Exec("UPDATE UserCharacterExtended SET nextInventorySlot=max(nextInventorySlot,?) WHERE userID=?", slot, id); e != nil {
		return nil, e
	}
	return touched, nil
}
