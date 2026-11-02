package server

import (
	"errors"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

const zbAddonMax = 6

// setZBAddons handles room sub-request 65/35. Upstream consumes one
// zbsaddonitem per requested ID and then replaces the stored addon list;
// unknown, unowned or protected items are skipped.
func (s *Server) setZBAddons(c *client, r *protocol.Reader) error {
	if s.rooms[c.room] == nil || c.playing {
		return nil
	}
	size := r.U16()
	if r.Err != nil {
		return r.Err
	}
	if size > zbAddonMax {
		return nil
	}
	protected := append([]int{}, s.Config.DefaultUser.DefaultItems...)
	protected = append(protected, s.Config.DefaultUser.PseudoDefaultItems...)
	addons := make([]int, 0, size)
	var changed []store.Item
	for i := 0; i < size; i++ {
		itemID := r.U16()
		if r.Err != nil {
			return r.Err
		}
		if s.items[itemID].ClassName != "zbsaddonitem" {
			continue
		}
		consumed, err := s.Store.ConsumeItem(c.user.ID, itemID, protected)
		if errors.Is(err, store.ErrItemUnavailable) || errors.Is(err, store.ErrItemProtected) {
			continue
		}
		if err != nil {
			return err
		}
		changed = append(changed, consumedUpdates(consumed, itemID)...)
		addons = append(addons, itemID)
	}
	if r.Pos != len(r.Data) {
		return errors.New("unexpected ZB addon tail")
	}
	if err := s.Store.SetAddons(c.user.ID, addons); err != nil {
		return err
	}
	if len(changed) == 0 {
		return nil
	}
	return s.sendInventoryItems(c, changed)
}

// consumedUpdates keeps the slots ConsumeItem touched: the updated stack with
// the consumed item ID plus any zeroed slot reported for a removed stack.
func consumedUpdates(items []store.Item, itemID int) []store.Item {
	updates := make([]store.Item, 0, len(items))
	for _, item := range items {
		if item.ID == itemID || item.ID == 0 {
			updates = append(updates, item)
		}
	}
	return updates
}

func (s *Server) addonRequest(c *client) error {
	addons, err := s.Store.Addons(c.user.ID)
	if err != nil {
		return err
	}
	if len(addons) > 65535 {
		return errors.New("addon list exceeds protocol range")
	}
	w := &protocol.Writer{}
	w.U16(len(addons))
	for _, addon := range addons {
		w.U16(addon)
	}
	c.send(115, w)
	return nil
}
