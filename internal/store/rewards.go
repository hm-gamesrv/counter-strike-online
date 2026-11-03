package store

import (
	"database/sql"
	"errors"
	"math"
)

type Grant struct {
	ClaimToken                             int
	Items                                  []Reward
	Points, Exp, HonorPoints, MinimumLevel int
	// A zero source grants a server-authorized reward without consuming an item.
	SourceSlot, SourceItem, Consume int
	Protected                       []int
}

var ErrRewardLevel = errors.New("Your level is too low for this reward.")

// validateGrant checks the bundle before any transaction is opened.
func validateGrant(g Grant, limit, defaultSlots int) error {
	if err := validateGrants(g.Items, limit, defaultSlots); err != nil {
		return err
	}
	for _, v := range []int{g.Points, g.Exp, g.HonorPoints} {
		if v < 0 || uint64(v) > math.MaxUint32 {
			return errors.New("reward outside protocol range")
		}
	}
	if g.MinimumLevel < 0 || g.SourceSlot < 0 || g.SourceSlot == 0 && (g.Consume != 0 || g.SourceItem != 0) || g.SourceSlot != 0 && (g.Consume < 1 || g.Consume > 65535 || g.SourceItem < 1) {
		return ErrItemUnavailable
	}
	return nil
}

// GrantReward commits source consumption, the complete bundle and character
// rewards together. Clients never supply this structure or choose their awards.
func (s *Store) GrantReward(id int, g Grant, limit, defaultSlots int) ([]Item, error) {
	if err := validateGrant(g, limit, defaultSlots); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	items, err := grantRewardTx(tx, id, g, limit, defaultSlots)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

// GrantRewardNotice consumes one offline reward notice and grants its bundle in
// the same transaction, so a crash between the two cannot pay it twice.
func (s *Store) GrantRewardNotice(id, reward int, g Grant, limit, defaultSlots int) ([]Item, error) {
	if err := validateGrant(g, limit, defaultSlots); err != nil {
		return nil, err
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := tx.Exec("DELETE FROM UserRewardNotice WHERE rowid IN (SELECT rowid FROM UserRewardNotice WHERE userID=? AND rewardID=? LIMIT 1)", id, reward)
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if n != 1 {
		return nil, ErrRewardClaimed
	}
	items, err := grantRewardTx(tx, id, g, limit, defaultSlots)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return items, nil
}

// grantRewardTx performs the whole grant inside the caller's transaction.
func grantRewardTx(tx *sql.Tx, id int, g Grant, limit, defaultSlots int) ([]Item, error) {
	var err error
	if g.ClaimToken != 0 {
		result, err := tx.Exec("UPDATE GoRewardClaim SET claimed=1 WHERE userID=? AND token=? AND claimed=0", id, g.ClaimToken)
		if err != nil {
			return nil, err
		}
		if n, err := result.RowsAffected(); err != nil {
			return nil, err
		} else if n != 1 {
			return nil, ErrRewardClaimed
		}
	}
	var level, exp int
	if err = tx.QueryRow("SELECT level,exp FROM UserCharacter WHERE userID=?", id).Scan(&level, &exp); err != nil {
		return nil, err
	}
	if level < g.MinimumLevel {
		return nil, ErrRewardLevel
	}
	oldLevel := level
	removed := false
	if g.SourceSlot != 0 {
		items, err := readInventory(tx, id)
		if err != nil {
			return nil, err
		}
		var source Item
		for _, item := range items {
			if item.Slot == g.SourceSlot {
				source = item
				break
			}
		}
		if source.ID != g.SourceItem || source.Count < g.Consume || source.InUse != 1 || source.Status != 0 {
			return nil, ErrItemUnavailable
		}
		if source.Lock != 0 {
			return nil, ErrItemProtected
		}
		for _, protected := range g.Protected {
			if protected == source.ID {
				return nil, ErrItemProtected
			}
		}
		if source.Count == g.Consume {
			_, err = tx.Exec("DELETE FROM UserInventory WHERE userID=? AND slot=?", id, source.Slot)
			removed = true
		} else {
			_, err = tx.Exec("UPDATE UserInventory SET count=count-? WHERE userID=? AND slot=?", g.Consume, id, source.Slot)
		}
		if err != nil {
			return nil, err
		}
	}
	if err = grantItems(tx, id, g.Items, limit, defaultSlots); err != nil {
		return nil, err
	}
	if exp < 0 {
		return nil, errors.New("negative character experience")
	}
	exp = min(exp, 30069814)
	exp += min(g.Exp, 30069814-exp)
	level = max(level, levelForExp(exp))
	result, err := tx.Exec("UPDATE UserCharacter SET points=points+?,honorPoints=honorPoints+?,exp=?,level=? WHERE userID=? AND points BETWEEN 0 AND ? AND honorPoints BETWEEN 0 AND ?", g.Points, g.HonorPoints, exp, level, id, int64(math.MaxInt64)-int64(g.Points), int64(math.MaxUint32)-int64(g.HonorPoints))
	if err != nil {
		return nil, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if n != 1 {
		return nil, errors.New("reward exceeds character counter range")
	}
	items, err := readInventory(tx, id)
	if err != nil {
		return nil, err
	}
	if removed {
		items = append([]Item{{Slot: g.SourceSlot}}, items...)
	}
	if level > oldLevel {
		if err = enqueueQuestEvent(tx, QuestEvent{UserID: id, Kind: 5, Count: level - oldLevel}); err != nil {
			return nil, err
		}
	}
	return items, nil
}
