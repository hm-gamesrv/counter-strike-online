package server

import (
	"errors"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

const zbAddonMax = 6

// setZBAddons handles room sub-request 65/35. The whole request is parsed and
// validated first, then the materials and the addon list are replaced in one
// transaction, so a rejected or failed request never eats items.
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
	requested := make([]int, 0, size)
	for i := 0; i < size; i++ {
		itemID := r.U16()
		if r.Err != nil {
			return r.Err
		}
		requested = append(requested, itemID)
	}
	if r.Pos != len(r.Data) {
		return errors.New("unexpected ZB addon tail")
	}
	accepted := make([]int, 0, len(requested))
	for _, itemID := range requested {
		if s.items[itemID].ClassName == "zbsaddonitem" {
			accepted = append(accepted, itemID)
		}
	}
	protected := append([]int{}, s.Config.DefaultUser.DefaultItems...)
	protected = append(protected, s.Config.DefaultUser.PseudoDefaultItems...)
	_, changed, err := s.Store.SetAddonsConsuming(c.user.ID, accepted, protected)
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		return nil
	}
	return s.sendInventoryItems(c, changed)
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
