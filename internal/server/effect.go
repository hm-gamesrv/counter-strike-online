package server

import (
	"errors"

	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// toggleEffectItem switches a cosmetic effect item and refreshes the inventory
// rows the store touched. Upstream sends no user-info packet for these effects.
func (s *Server) toggleEffectItem(c *client, item store.Item, column string) error {
	changed, err := s.Store.ToggleEffectItem(c.user.ID, item.ID, item.Slot, column)
	if errors.Is(err, store.ErrItemUnavailable) {
		c.notice("Item unavailable.")
		return nil
	}
	if err != nil {
		return err
	}
	return s.sendInventoryItems(c, changed)
}

// toggleNameplate switches a LobbyBG nameplate and republishes the user info
// because the nameplate travels in the 157 nameplate block.
func (s *Server) toggleNameplate(c *client, item store.Item) error {
	changed, nameplate, err := s.Store.ToggleNameplate(c.user.ID, item.ID, item.Slot)
	if errors.Is(err, store.ErrItemUnavailable) {
		c.notice("Item unavailable.")
		return nil
	}
	if err != nil {
		return err
	}
	c.character.NameplateID = nameplate
	if err = s.sendInventoryItems(c, changed); err != nil {
		return err
	}
	s.broadcastUserInfo(c)
	return nil
}
