package server

import (
	"encoding/binary"
	"errors"
	"net"
	"sort"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

// settings retains individual flag fields verbatim, allowing incremental
// updates without losing fields the lobby itself does not interpret.
type settings struct {
	flags  [4]uint32
	fields [128][]byte
}

func parseSettings(r *protocol.Reader) (settings, error) {
	var s settings
	for i := range s.flags {
		s.flags[i] = uint32(r.U32())
	}
	if r.Err != nil {
		return s, r.Err
	}
	for group, mask := range s.flags {
		for bit := 0; bit < 32; bit++ {
			if mask&(1<<bit) == 0 {
				continue
			}
			start := r.Pos
			switch group {
			case 0:
				switch bit {
				case 0, 3:
					r.CString()
				case 2:
					r.Bytes(7)
				case 7, 10:
					r.U16()
				case 13:
					if r.U8() == 18 {
						r.Bytes(64)
					}
				case 31:
					r.Bytes(35)
				default:
					r.U8()
				}
			case 1:
				switch bit {
				case 0:
					r.U32()
					r.CString()
					r.Bytes(3)
				case 2:
					r.Bytes(5)
				case 4, 16:
					r.U16()
				case 9:
					n := r.U8()
					r.Bytes(n * 3)
				case 13:
					r.Bytes(9)
				case 17:
					r.Bytes(17)
				case 18:
					parseVoxel(r)
				case 19, 31: // Unused flag positions in the reference.
				case 20:
					n := r.U8()
					r.Bytes(n * 2)
				default:
					r.U8()
				}
			case 2:
				switch bit {
				case 1:
					r.Bytes(5)
				case 9:
					r.U32()
					r.CString()
					r.CString()
					r.U32()
				default:
					if bit <= 8 {
						r.U8()
					}
				}
			case 3:
				if bit == 31 {
					r.U8()
				}
			}
			if r.Err != nil {
				return s, r.Err
			}
			s.fields[group*32+bit] = append([]byte{}, r.Data[start:r.Pos]...)
		}
	}
	return s, nil
}
func parseVoxel(r *protocol.Reader) {
	f := uint32(r.U32())
	for bit := 0; bit < 25; bit++ {
		if f&(1<<bit) == 0 {
			continue
		}
		switch bit {
		case 0, 1, 3, 6, 7, 8, 9, 10, 22:
			r.CString()
		case 2, 4, 5, 19, 21, 23, 24:
			r.U8()
		case 14:
			n := r.U32()
			if n > 4096 {
				r.Err = errors.New("voxel array too large")
				return
			}
			for i := 0; i < n && r.Err == nil; i++ {
				r.U32()
				r.CString()
			}
		default:
			r.U32()
		}
	}
}
func (s *settings) write(w *protocol.Writer) {
	for _, v := range s.flags {
		w.U32(int(v))
	}
	for group, mask := range s.flags {
		for bit := 0; bit < 32; bit++ {
			if mask&(1<<bit) != 0 {
				w.Bytes(s.fields[group*32+bit])
			}
		}
	}
}
func (s *settings) number(index int) int {
	b := s.fields[index]
	switch len(b) {
	case 1:
		return int(b[0])
	case 2:
		return int(binary.LittleEndian.Uint16(b))
	default:
		return 0
	}
}
func (s *settings) text(index int) string {
	b := s.fields[index]
	if len(b) == 0 {
		return ""
	}
	return string(b[:len(b)-1])
}
func (s *settings) merge(update settings) {
	for i := range s.flags {
		s.flags[i] |= update.flags[i]
	}
	for i, b := range update.fields {
		if b != nil {
			s.fields[i] = b
		}
	}
}

type room struct {
	id        int
	host      *client
	members   []*client
	settings  settings
	match     *gameMatch
	dedicated *dedicatedServer
	kicked    map[int]bool
}

func (s *Server) roomList(c *client) {
	w := &protocol.Writer{}
	w.U8(0)
	w.U8(4)
	w.U16(65535)
	w.U16(0)
	w.U16(1)
	w.U16(0)
	w.U16(2)
	w.U16(1)
	w.U8(len(s.rooms))
	ids := make([]int, 0, len(s.rooms))
	for id := range s.rooms {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		room := s.rooms[id]
		w.U8(0)
		w.U32(id)
		w.U8(4)
		w.U8(0)
		w.U16(0x0e3e)
		w.U32(1 | 4 | 16 | 32 | 64 | 128)
		w.U32(0)
		w.String(room.settings.text(0))
		if room.settings.text(3) != "" {
			w.U8(1)
		} else {
			w.U8(0)
		}
		w.U8(room.settings.number(6))
		w.U16(room.settings.number(7))
		w.U8(len(room.members))
		w.U8(room.settings.number(8))
	}
	c.send(151, w)
}
func (s *Server) refreshRooms() {
	for c := range s.clients {
		if c.inLobby {
			s.roomList(c)
		}
	}
}
func networkInfo(w *protocol.Writer, c *client) {
	w.U8(c.team)
	w.U8(0)
	w.U8(0)
	w.U8(0)
	ip := net.ParseIP(c.ip()).To4()
	if ip == nil {
		ip = net.IPv4zero.To4()
	}
	w.Bytes(ip)
	w.U16(c.externalPorts[0])
	w.U16(c.externalPorts[1])
	ip = c.localIP.To4()
	if ip == nil {
		ip = net.IPv4zero.To4()
	}
	w.Bytes(ip)
	w.U16(c.localPorts[0])
	w.U16(c.localPorts[1])
}
func roomUser(w *protocol.Writer, c *client) {
	w.U32(c.user.ID)
	w.U32(0)
	w.String(c.user.Name)
	networkInfo(w, c)
	userInfo(w, c.character)
}
func (r *room) broadcast(id byte, w *protocol.Writer) {
	for _, c := range r.members {
		c.send(id, w)
	}
	if r.dedicated != nil {
		r.dedicated.client.send(id, w)
	}
}
func (r *room) joined(c *client) {
	w := &protocol.Writer{}
	w.U8(0)
	w.U8(0)
	w.U32(r.id)
	w.U32(r.host.user.ID)
	w.U16(0xb5)
	w.U8(255)
	r.settings.write(w)
	w.U8(len(r.members))
	for _, u := range r.members {
		roomUser(w, u)
	}
	c.send(65, w)
	for _, u := range r.members {
		w = &protocol.Writer{}
		w.U8(3)
		w.U32(u.user.ID)
		w.U8(u.ready)
		c.send(65, w)
	}
}
func (s *Server) roomRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	if r.Err != nil {
		return r.Err
	}
	currentRoom := s.rooms[c.room]
	switch kind {
	case 0:
		if !c.inLobby || currentRoom != nil {
			return errors.New("must be in lobby to create room")
		}
		st, e := parseSettings(r)
		if e != nil {
			return e
		}
		if r.Pos != len(r.Data) {
			return errors.New("unexpected room settings tail")
		}
		if st.number(8) < 1 || st.number(8) > 32 || len(st.text(0)) < 1 || len(st.text(0)) > 64 || len(st.text(3)) > 32 {
			return errors.New("invalid room capacity/name/password")
		}
		if len(s.rooms) >= 255 {
			c.notice("Room limit reached.")
			return nil
		}
		if st.number(70) != 0 || st.number(7) == 254 {
			c.notice("Go migration: clan battles and Studio rooms are not implemented yet.")
			return nil
		}
		if !s.roomDefaults(&st, true) {
			c.notice("Unsupported game mode.")
			return nil
		}
		if err := s.validateRoomSettings(&st, 1); err != nil {
			c.notice("Invalid room settings.")
			return nil
		}
		if err := validateRoomFrame(st); err != nil {
			return err
		}
		id := s.nextRoom
		s.nextRoom++
		if id > 65535 {
			for id = 1; s.rooms[id] != nil; id++ {
			}
			s.nextRoom = id + 1
		}
		currentRoom = &room{id: id, host: c, members: []*client{c}, settings: st}
		s.rooms[id] = currentRoom
		c.room = id
		c.team = 2
		c.ready = 0
		s.lobbyLeft(c)
		currentRoom.joined(c)
		s.refreshRooms()
	case 23:
		r.U8()
		id := r.U16()
		password := r.CString()
		if r.Err != nil {
			return r.Err
		}
		if !c.inLobby || currentRoom != nil {
			return errors.New("already in room or not in lobby")
		}
		to := s.rooms[id]
		if to == nil {
			c.notice("ROOM_JOIN_FAILED_CLOSED")
			return nil
		}
		if to.kicked[c.user.ID] {
			c.notice("ROOM_JOIN_FAILED_KICKED")
			return nil
		}
		if len(to.members) >= to.settings.number(8) {
			c.notice("ROOM_JOIN_FAILED_FULL")
			return nil
		}
		if to.settings.text(3) != "" && to.settings.text(3) != password {
			c.notice("ROOM_JOIN_FAILED_INVALID_PASSWD")
			return nil
		}
		allowed, err := s.hostAllows(to.host, c)
		if err != nil {
			if s.Log != nil {
				s.Log.Warn("host ban list read failed", "user", to.host.user.ID, "error", err)
			}
			c.notice("DB_QUERY_FAILED")
			return nil
		}
		if !allowed {
			c.notice("ROOM_JOIN_FAILED_BAN")
			return nil
		}
		c.room = id
		c.team = 2
		c.ready = 0
		w := &protocol.Writer{}
		w.U8(1)
		roomUser(w, c)
		to.broadcast(65, w)
		to.members = append(to.members, c)
		s.lobbyLeft(c)
		to.joined(c)
		s.refreshRooms()
	case 2:
		s.leaveRoom(c)
		s.joinLobby(c)
		return s.resumeQuestRewards(c)
	case 3:
		if currentRoom != nil && currentRoom.match == nil && currentRoom.host != c {
			if c.ready == 0 {
				c.ready = 2
			} else {
				c.ready = 0
			}
			w := &protocol.Writer{}
			w.U8(3)
			w.U32(c.user.ID)
			w.U8(c.ready)
			currentRoom.broadcast(65, w)
		}
	case 17:
		team := r.U8()
		if r.Err != nil {
			return r.Err
		}
		if currentRoom != nil && !c.playing && c.ready == 0 && team >= 1 && team <= 2 {
			c.team = team
			w := &protocol.Writer{}
			w.U8(13)
			w.U32(c.user.ID)
			w.U8(team)
			currentRoom.broadcast(65, w)
		}
	case 5:
		if currentRoom == nil || currentRoom.host != c || currentRoom.match != nil {
			return errors.New("only host can update settings")
		}
		st, e := parseSettings(r)
		if e != nil {
			return e
		}
		merged := currentRoom.settings
		if st.fields[6] != nil && st.number(6) != merged.number(6) {
			if !s.roomDefaults(&st, false) {
				c.notice("Unsupported game mode.")
				return nil
			}
		}
		merged.merge(st)
		if r.Pos != len(r.Data) || merged.number(8) < len(currentRoom.members) || merged.number(8) > 32 || len(merged.text(0)) < 1 || len(merged.text(0)) > 64 || len(merged.text(3)) > 32 {
			return errors.New("invalid room settings")
		}
		if merged.number(70) != 0 || merged.number(7) == 254 {
			c.notice("Go migration: clan battles and Studio rooms are not implemented yet.")
			return nil
		}
		if err := s.validateRoomSettings(&merged, len(currentRoom.members)); err != nil {
			c.notice("Invalid room settings.")
			return nil
		}
		if err := validateRoomFrame(merged); err != nil {
			return err
		}
		currentRoom.settings = merged
		w := &protocol.Writer{}
		w.U8(4)
		st.write(w)
		currentRoom.broadcast(65, w)
		s.refreshRooms()
	case 22:
		s.roomList(c)
	case 4:
		return s.startGame(c)
	case 6:
		c.send(68, &protocol.Writer{Data: []byte{4}})
		w := &protocol.Writer{}
		w.U32(c.user.ID)
		userInfo(w, c.character)
		c.send(157, w)
		return s.resumeQuestRewards(c)
	case 7:
		id := r.U32()
		if r.Err != nil {
			return r.Err
		}
		return s.kickRoomUser(c, id)
	case 15:
		if currentRoom != nil {
			s.roomInviteList(c)
		}
	case 19:
		return s.inviteRoomUsers(c, r)
	case 11:
		if currentRoom != nil && c.playing {
			c.playing = false
			c.ready = 0
			c.notice("ROOM_JOIN_FAILED_INVALID_GAME_IP")
			s.broadcastReady(currentRoom)
		}
	default:
		c.notice("Go migration: this room action is not implemented yet.")
	}
	return r.Err
}

func (s *Server) leaveRoom(c *client) {
	r := s.rooms[c.room]
	if r == nil {
		return
	}
	if c.playing {
		c.send(65, &protocol.Writer{Data: []byte{10}})
	}
	c.playing = false
	if r.match != nil {
		delete(r.match.stats, c.user.ID)
	}
	for i, u := range r.members {
		if u == c {
			r.members = append(r.members[:i], r.members[i+1:]...)
			break
		}
	}
	c.room = 0
	c.ready = 0
	w := &protocol.Writer{}
	w.U8(2)
	w.U32(c.user.ID)
	r.broadcast(65, w)
	if len(r.members) == 0 {
		releaseDedicated(r)
		delete(s.rooms, r.id)
	} else if r.host == c {
		r.kicked = nil
		r.host = r.members[0]
		for _, u := range r.members {
			if u.playing {
				r.host = u
				break
			}
		}
		w = &protocol.Writer{}
		w.U8(5)
		w.U32(r.host.user.ID)
		w.U8(1)
		r.broadcast(65, w)
		s.migrateHost(r)
	}
	s.refreshRooms()
}
