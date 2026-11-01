package server

import (
	"errors"
	"sort"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func (s *Server) roomInviteList(c *client) {
	var available []*client
	for _, u := range s.users {
		if u.inLobby {
			available = append(available, u)
		}
	}
	sort.Slice(available, func(i, j int) bool { return available[i].user.ID < available[j].user.ID })
	w := &protocol.Writer{}
	w.U8(12)
	w.U16(len(available))
	for _, u := range available {
		w.U32(u.user.ID)
		userInfo(w, u.character)
	}
	c.send(65, w)
}

func (s *Server) inviteRoomUsers(c *client, p *protocol.Reader) error {
	message := p.CString()
	n := p.U16()
	if p.Err != nil {
		return p.Err
	}
	if n > 256 || len(message) > 1024 {
		return errors.New("room invitation too large")
	}
	names := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		name := p.CString()
		if len(name) > 64 {
			return errors.New("invitation nickname too long")
		}
		names[name] = true
	}
	if p.Err != nil {
		return p.Err
	}
	r := s.rooms[c.room]
	if r == nil {
		return nil
	}
	w := &protocol.Writer{}
	w.U8(3)
	w.U8(1) // Server and channel IDs match the single-channel lobby.
	w.U8(1)
	w.U16(r.id)
	w.U8(r.settings.number(6))
	w.U8(r.settings.number(7)) // This legacy invitation field is one byte upstream.
	w.U8(r.settings.number(8))
	w.Bytes([]byte{9, 10, 11, 1, 22, 33, 1})
	w.String("228 unk str")
	w.String(c.character.Name)
	w.String(message)
	for _, u := range s.users {
		if u != c && u.character.Name != "" && names[u.character.Name] {
			u.send(80, w)
		}
	}
	return nil
}

func (s *Server) kickRoomUser(c *client, id int) error {
	r := s.rooms[c.room]
	u := s.users[id]
	if r == nil || r.host != c || r.match != nil || c.playing || u == nil || u == c || u.room != r.id {
		c.notice("Only the waiting room host can kick another member.")
		return nil
	}
	if r.kicked == nil {
		r.kicked = map[int]bool{}
	}
	r.kicked[id] = true
	s.leaveRoom(u)
	w := &protocol.Writer{}
	w.U8(7)
	w.U32(id)
	u.send(65, w)
	s.joinLobby(u)
	return nil
}
