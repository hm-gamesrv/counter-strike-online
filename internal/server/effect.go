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
