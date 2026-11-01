package server

import (
	"database/sql"
	"errors"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func (s *Server) messengerRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	if kind != 1 {
		s.Log.Debug("unsupported messenger request", "kind", kind, "user", c.user.ID)
		return r.Err
	}
	name := r.CString()
	if r.Err != nil {
		return r.Err
	}
	if r.Pos != len(r.Data) {
		return errors.New("unexpected messenger request tail")
	}
	target, err := s.Store.CharacterByName(name)
	if errors.Is(err, sql.ErrNoRows) {
		target = store.Character{Name: name}
	} else if err != nil {
		return err
	}
	w := &protocol.Writer{}
	w.U8(1)
	w.U32(target.ID)
	w.String("22223123132")
	userInfo(w, target)
	c.send(93, w)
	return nil
}
