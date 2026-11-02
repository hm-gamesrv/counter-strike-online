package server

import (
	"errors"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func (s *Server) kickRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	if kind != 1 {
		return r.Err
	}
	id := r.U32()
	r.U8()
	r.CString()
	if r.Err != nil {
		return r.Err
	}
	if r.Pos != len(r.Data) {
		return errors.New("unexpected kick request tail")
	}
	room := s.rooms[c.room]
	target := s.users[id]
	if room == nil || room.host != c || room.settings.number(58) == 0 || target == nil || target == c || target.room != room.id {
		c.notice("Only a superRoom host can kick another member.")
		return nil
	}
	if room.kicked == nil {
		room.kicked = map[int]bool{}
	}
	room.kicked[id] = true
	w := &protocol.Writer{}
	w.U8(3)
	w.U32(id)
	w.U8(1)
	room.broadcast(112, w)
	s.leaveRoomRequest(target)
	s.joinLobby(target)
	return nil
}
