package server

import (
	"errors"
	"slices"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// A title can only be selected after it has been unlocked. The upstream server
// checks the matching honor quest instead, but Go quests only cover events and
// the persisted achievement list is the durable source of unlocked titles.
func (s *Server) titleUnlocked(c *client, title int) bool {
	return title == 0 || slices.Contains(c.character.Achievements, title)
}

func (s *Server) titleRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	switch kind {
	case 0:
		title := r.U16()
		if r.Err != nil {
			return r.Err
		}
		if r.Pos != len(r.Data) {
			return errors.New("unexpected title request tail")
		}
		if !s.titleUnlocked(c, title) {
			return nil
		}
		if err := s.Store.SetPrefix(c.user.ID, title); err != nil {
			return err
		}
		c.character.PrefixID = title
		s.broadcastUserInfo(c)
	case 1:
		slot, title := r.U8(), r.U16()
		if r.Err != nil {
			return r.Err
		}
		if r.Pos != len(r.Data) {
			return errors.New("unexpected title list request tail")
		}
		if slot >= store.TitleSlots || !s.titleUnlocked(c, title) {
			return nil
		}
		if err := s.Store.SetTitle(c.user.ID, slot, title); err != nil {
			return err
		}
		c.character.Titles[slot] = title
		s.broadcastUserInfo(c)
	case 2:
		slot := r.U8()
		if r.Err != nil {
			return r.Err
		}
		if r.Pos != len(r.Data) || slot >= store.TitleSlots {
			return r.Err
		}
		if err := s.Store.SetTitle(c.user.ID, slot, 0); err != nil {
			return err
		}
		c.character.Titles[slot] = 0
		s.broadcastUserInfo(c)
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported title request", "kind", kind, "user", c.user.ID)
		}
		return r.Err
	}
	return nil
}
