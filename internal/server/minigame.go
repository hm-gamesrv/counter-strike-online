package server

import (
	"errors"
	"fmt"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

const weaponReleaseEventFlag = 1 << 6

type weaponReleaseRow struct {
	ItemID   int
	Count    int
	Duration int
	Name     string
}

type weaponReleaseConfig struct {
	Active     bool
	Rows       []weaponReleaseRow
	Characters []int
}

func loadWeaponRelease(cfg config.MiniGameWeaponRelease) (weaponReleaseConfig, error) {
	result := weaponReleaseConfig{Active: cfg.Active}
	if len(cfg.Items) == 0 && len(cfg.Characters) == 0 {
		return result, nil
	}
	if len(cfg.Items) > 255 || len(cfg.Characters) > 255 {
		return result, errors.New("weapon release rows and characters must fit uint8")
	}
	for _, item := range cfg.Items {
		if item.ID < 1 || item.ID > 65535 {
			return result, fmt.Errorf("weapon release item %d outside uint16", item.ID)
		}
		if item.Count < 1 || item.Count > 255 {
			return result, fmt.Errorf("weapon release item %d count outside uint8", item.ID)
		}
		if item.Duration < 0 || item.Duration > 255 {
			return result, fmt.Errorf("weapon release item %d duration outside uint8", item.ID)
		}
		if len(item.Name) < 1 || len(item.Name) > 8 {
			return result, fmt.Errorf("weapon release item %d name must be 1..8 bytes", item.ID)
		}
		result.Rows = append(result.Rows, weaponReleaseRow{
			ItemID:   item.ID,
			Count:    item.Count,
			Duration: item.Duration,
			Name:     item.Name,
		})
	}
	for _, character := range cfg.Characters {
		if len(character) != 1 {
			return result, fmt.Errorf("weapon release character %q must be one byte", character)
		}
		result.Characters = append(result.Characters, int(character[0]))
	}
	if result.Active && (len(result.Rows) == 0 || len(result.Characters) == 0) {
		return result, errors.New("active weapon release needs rows and characters")
	}
	return result, nil
}

func (s *Server) eventFlags() int {
	if s.miniGame.Active {
		return weaponReleaseEventFlag
	}
	return 0
}

func (s *Server) miniGameRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	if kind != 6 {
		if s.Log != nil {
			s.Log.Debug("unsupported mini game", "kind", kind, "user", c.user.ID)
		}
		return r.Err
	}
	request := r.U8()
	switch request {
	case 60:
		return s.sendWeaponReleaseUpdate(c)
	case 61:
		return s.weaponReleaseSet(c, r)
	case 62:
		return r.Err
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported weapon release request", "request", request, "user", c.user.ID)
		}
	}
	return r.Err
}

func (s *Server) sendWeaponReleaseUpdate(c *client) error {
	rows, err := s.Store.WeaponReleaseRows(c.user.ID)
	if err != nil {
		return err
	}
	characters, total, err := s.Store.WeaponReleaseCharacters(c.user.ID)
	if err != nil {
		return err
	}
	byItem := map[int]store.WeaponReleaseRow{}
	for _, row := range rows {
		byItem[row.ItemID] = row
	}
	byCharacter := map[int]int{}
	for _, character := range characters {
		byCharacter[character.Character] = character.Count
	}
	status := 2
	if s.miniGame.Active {
		status = 1
	}
	w := &protocol.Writer{}
	w.U8(6)
	w.U8(65)
	w.U8(status)
	w.U8(len(s.miniGame.Rows))
	for _, row := range s.miniGame.Rows {
		w.U16(row.ItemID)
		w.U8(row.Count)
		w.U8(row.Duration)
		w.String(row.Name)
		progress := byItem[row.ItemID]
		w.U8(progress.Progress)
		w.U8(progress.Opened)
	}
	w.U8(len(s.miniGame.Characters))
	for _, character := range s.miniGame.Characters {
		w.U8(character)
		w.U8(min(byCharacter[character], 255))
	}
	if total > 65535 {
		total = 65535
	}
	w.U16(total)
	c.send(89, w)
	return nil
}

func (s *Server) weaponReleaseSet(c *client, r *protocol.Reader) error {
	weaponSlot := r.U8()
	slot := r.U8()
	charID := r.U8()
	if r.Err != nil {
		return r.Err
	}
	if !s.miniGame.Active {
		s.sendWeaponReleaseSet(c, 3, 0, 0, 0, 0)
		return nil
	}
	if weaponSlot >= len(s.miniGame.Rows) || slot >= len(s.miniGame.Rows[weaponSlot].Name) {
		return nil
	}
	row := s.miniGame.Rows[weaponSlot]
	bit := len(row.Name) - (slot + 1)
	if bit < 0 || bit > 7 {
		return nil
	}
	if int(row.Name[bit]) != int(charID) {
		return nil
	}
	rows, err := s.Store.WeaponReleaseRows(c.user.ID)
	if err != nil {
		return err
	}
	progress := store.WeaponReleaseRow{ItemID: row.ItemID}
	for _, candidate := range rows {
		if candidate.ItemID == row.ItemID {
			progress = candidate
			break
		}
	}
	if progress.Opened != 0 {
		return nil
	}
	if progress.Progress&(1<<bit) != 0 {
		s.sendWeaponReleaseSet(c, 5, 0, 0, 0, 0)
		return nil
	}
	character := int(charID)
	characters, _, err := s.Store.WeaponReleaseCharacters(c.user.ID)
	if err != nil {
		return err
	}
	available := false
	for _, candidate := range characters {
		if candidate.Character == character && candidate.Count > 0 {
			available = true
			break
		}
	}
	if !available {
		character = int('~')
		for _, candidate := range characters {
			if candidate.Character == character && candidate.Count > 0 {
				available = true
				break
			}
		}
	}
	if !available {
		s.sendWeaponReleaseSet(c, 4, 0, 0, 0, 0)
		return nil
	}
	opened := progress.Progress|(1<<bit) == (1<<len(row.Name))-1
	code, _, left, err := s.Store.SetWeaponReleaseCharacter(c.user.ID, row.ItemID, bit, character, opened)
	if err != nil {
		return err
	}
	switch code {
	case -1, -2, -3:
		s.sendWeaponReleaseSet(c, 5, 0, 0, 0, 0)
		return nil
	}
	if opened {
		if err = s.grantWeaponRelease(c, row); err != nil {
			if s.Log != nil {
				s.Log.Warn("weapon release grant failed", "user", c.user.ID, "item", row.ItemID, "error", err)
			}
			c.notice("Inventory is full.")
		}
	}
	status := 0
	if opened {
		status = 2
	}
	s.sendWeaponReleaseSet(c, status, weaponSlot, slot, character, left)
	return nil
}

func (s *Server) grantWeaponRelease(c *client, row weaponReleaseRow) error {
	items := []store.Reward{{
		ID:       row.ItemID,
		Count:    row.Count,
		Duration: row.Duration,
	}}
	items, err := s.prepareGrants(items)
	if err != nil {
		return err
	}
	changed, err := s.Store.GrantReward(c.user.ID, store.Grant{Items: items}, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
	if err != nil {
		return err
	}
	if err = s.sendInventoryItems(c, changed); err != nil {
		return err
	}
	notice := rewardNotice(itemReward{
		Title:       "Weapon Release Event",
		Description: "Congratulations! You have received the below weapon by collecting the word.",
	}, items, store.Grant{})
	if len(notice.Data) <= protocol.MaxPayload {
		c.send(67, notice)
	}
	return nil
}

func (s *Server) sendWeaponReleaseSet(c *client, status, weaponSlot, slot, character, left int) {
	w := &protocol.Writer{}
	w.U8(6)
	w.U8(66)
	w.U8(status)
	if status <= 2 {
		if status == 2 {
			w.U8(0)
		} else {
			w.U8(1)
		}
		w.U8(weaponSlot)
		w.U8(slot)
		w.U8(character)
		w.U8(left)
	}
	c.send(89, w)
}
