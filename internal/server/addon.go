package server

import (
	"errors"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

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
