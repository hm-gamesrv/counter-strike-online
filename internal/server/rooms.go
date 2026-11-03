package server

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

// settings retains individual flag fields verbatim, allowing incremental
// updates without losing fields the lobby itself does not interpret.
type settings struct {
	flags       [4]uint32
	fields      [128][]byte
	truncated   bool
	truncatedAt int
}

func parseSettings(r *protocol.Reader) (settings, error) {
	var s settings
	for i := range s.flags {
		s.flags[i] = uint32(r.U32())
	}
	if r.Err != nil {
		return s, r.Err
	}
parse:
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
					// The client writes a single difficulty byte but reads nine
					// back (difficulty plus the reference's unk56/unk57), so the
					// stored field is padded to nine bytes below.
					r.U8()
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
				// CRoomSettings reads past the end of the buffer without failing,
				// and modified clients do send settings this table does not
				// describe. Keep the fields parsed so far instead of dropping the
				// connection, and record where the layout diverged.
				s.truncated = true
				s.truncatedAt = group*32 + bit
				r.Err = nil
				r.Pos = len(r.Data)
				break parse
			}
			field := append([]byte{}, r.Data[start:r.Pos]...)
			if group == 1 && bit == 13 && len(field) < 9 {
				field = append(field, make([]byte, 9-len(field))...)
			}
			s.fields[group*32+bit] = field
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

// voxelID returns the Studio slot ID from the room settings Voxel block.
func (s *settings) voxelID() string {
	b := s.fields[50]
	if len(b) < 4 {
		return ""
	}
	flag := binary.LittleEndian.Uint32(b)
	if flag&1 == 0 {
		return ""
	}
	r := &protocol.Reader{Data: b[4:]}
	return r.CString()
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

// layout renders the length of every transmitted field so a mismatched
// settings payload can be compared against the client's own bytes.
func (s *settings) layout() string {
	var b strings.Builder
	for group, mask := range s.flags {
		for bit := 0; bit < 32; bit++ {
			if mask&(1<<bit) == 0 {
				continue
			}
			fmt.Fprintf(&b, "%d:%d ", group*32+bit, len(s.fields[group*32+bit]))
		}
	}
	return strings.TrimSpace(b.String())
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
	clan1     int
	clan2     int
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
		writeRoomListEntry(w, s.rooms[id])
	}
	c.send(151, w)
}

// rotateRoomMap advances the map playlist after a match, mirroring
// CRoom::EndGame. Forced ends restart at the first map.
func (s *Server) rotateRoomMap(r *room, forced bool) bool {
	playlist := r.settings.playlist()
	if len(playlist) == 0 {
		return false
	}
	next := 0
	index := 1
	if !forced {
		current := r.settings.number(7)
		found := false
		for i, id := range playlist {
			if id == current {
				next = (i + 1) % len(playlist)
				found = true
				break
			}
		}
		if !found {
			return false
		}
		index = next + 1
	}
	mapID := playlist[next]
	r.settings.setNumber(7, mapID, 2)
	r.settings.setNumber(48, mapID, 2)
	r.settings.setNumber(42, index, 1)
	w := &protocol.Writer{}
	w.U8(4)
	w.U32(1 << 7)
	w.U32(1<<10 | 1<<16)
	w.U32(0)
	w.U32(0)
	w.U16(mapID)
	w.U8(index)
	w.U16(mapID)
	r.broadcast(65, w)
	return true
}

// writeRoomListEntry writes the per-room block shared by the full room list
// and the Voxel room list.
func writeRoomListEntry(w *protocol.Writer, room *room) {
	w.U8(0)
	w.U32(room.id)
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

// voxelRoomList handles room sub-request 65/34: the client asks for rooms
// bound to one Voxel slot ID.
func (s *Server) voxelRoomList(c *client, r *protocol.Reader) error {
	voxelID := r.CString()
	if r.Err != nil {
		return r.Err
	}
	ids := make([]int, 0, len(s.rooms))
	for id, room := range s.rooms {
		if room.settings.voxelID() == voxelID {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	w := &protocol.Writer{}
	w.U8(34)
	w.U8(len(ids))
	for _, id := range ids {
		writeRoomListEntry(w, s.rooms[id])
	}
	c.send(65, w)
	return nil
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
	w.U16(c.externalPorts[1])
	w.U16(c.externalPorts[0])
	ip = c.localIP.To4()
	if ip == nil {
		ip = net.IPv4zero.To4()
	}
	w.Bytes(ip)
	w.U16(c.localPorts[1])
	w.U16(c.localPorts[0])
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
			s.Log.Debug("room settings parse failed", "kind", kind, "len", len(r.Data), "hex", hex.EncodeToString(r.Data))
			c.notice("Unable to create a room due to incorrect settings")
			return nil
		}
		if st.truncated {
			// Applying half a settings packet would leave the server and client
			// disagreeing about every following field, so the request is refused.
			s.Log.Warn("room settings truncated", "kind", kind, "at", st.truncatedAt, "len", len(r.Data), "hex", hex.EncodeToString(r.Data), "layout", st.layout())
			c.notice("Unable to create a room due to incorrect settings")
			return nil
		}
		if r.Pos != len(r.Data) {
			s.Log.Debug("room settings tail", "kind", kind, "len", len(r.Data), "pos", r.Pos, "hex", hex.EncodeToString(r.Data))
		}
		s.Log.Debug("room settings", "kind", kind, "len", len(r.Data), "flags", st.flags, "layout", st.layout())
		if st.number(8) < 1 || st.number(8) > 32 || len(st.text(0)) < 1 || len(st.text(0)) > 64 || len(st.text(3)) > 32 {
			c.notice("Unable to create a room due to incorrect settings")
			return nil
		}
		if len(s.rooms) >= 255 {
			c.notice("Room limit reached.")
			return nil
		}
		if st.number(7) == 254 {
			// Studio creation needs CVoxelManager::GetSlotDetails (an HTTP slot
			// lookup) to override the room settings; without that service upstream
			// rejects the request too, so this reports the same failure.
			c.notice("Unable to create a room due to incorrect settings")
			return nil
		}
		familyBattle := st.number(70) != 0
		familyClan := 0
		if familyBattle {
			var err error
			familyClan, err = s.clanIDFor(c)
			if err != nil {
				return err
			}
			if familyClan == 0 {
				c.notice("ROOM_OPEN_FAILED_CLAN_NOT_MEMBER")
				return nil
			}
		}
		if !s.roomDefaults(&st, true) {
			c.notice("Unsupported game mode.")
			return nil
		}
		if err := s.refreshHostRoomItems(c, &st); err != nil {
			return err
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
		if familyBattle {
			currentRoom.clan1 = familyClan
		}
		s.rooms[id] = currentRoom
		c.room = id
		c.team = 2
		c.ready = 0
		s.lobbyLeft(c)
		currentRoom.joined(c)
		s.refreshRooms()
		if familyBattle {
			s.sendClanBattleNotice(c.user.ID, 0, c.character.Name, st.number(6), id)
		}
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
		if to.settings.number(70) != 0 && !s.familyBattleJoin(to, c) {
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
		if to.settings.number(70) != 0 {
			s.sendClanBattleNotice(c.user.ID, 1, c.character.Name, to.settings.number(6), id)
		}
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
		if currentRoom != nil && currentRoom.settings.number(70) != 0 {
			c.notice("You cannot move teams in Family Battle.")
			return nil
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
			s.Log.Debug("room settings parse failed", "kind", kind, "len", len(r.Data), "hex", hex.EncodeToString(r.Data))
			c.notice("Unable to create a room due to incorrect settings")
			return nil
		}
		if st.truncated {
			s.Log.Warn("room settings truncated", "kind", kind, "at", st.truncatedAt, "len", len(r.Data), "hex", hex.EncodeToString(r.Data), "layout", st.layout())
			c.notice("Unable to create a room due to incorrect settings")
			return nil
		}
		s.Log.Debug("room settings", "kind", kind, "len", len(r.Data), "flags", st.flags, "layout", st.layout())
		merged := currentRoom.settings
		if st.fields[6] != nil && st.number(6) != merged.number(6) {
			if !s.roomDefaults(&st, false) {
				c.notice("Unsupported game mode.")
				return nil
			}
		}
		merged.merge(st)
		if err := s.refreshHostRoomItems(currentRoom.host, &merged); err != nil {
			return err
		}
		if r.Pos != len(r.Data) {
			s.Log.Debug("room settings tail", "kind", kind, "len", len(r.Data), "pos", r.Pos, "hex", hex.EncodeToString(r.Data))
		}
		if merged.number(8) < len(currentRoom.members) || merged.number(8) > 32 || len(merged.text(0)) < 1 || len(merged.text(0)) > 64 || len(merged.text(3)) > 32 {
			c.notice("Unable to create a room due to incorrect settings")
			return nil
		}
		if merged.number(7) == 254 {
			c.notice("Unable to create a room due to incorrect settings")
			return nil
		}
		if merged.number(70) != 0 {
			candidate := &room{host: currentRoom.host, members: currentRoom.members, settings: merged}
			if !s.familyBattleInit(candidate, candidate.host) {
				return nil
			}
			for _, member := range candidate.members {
				if member == candidate.host {
					continue
				}
				if !s.familyBattleJoin(candidate, member) {
					return nil
				}
			}
			currentRoom.clan1 = candidate.clan1
			currentRoom.clan2 = candidate.clan2
		} else {
			currentRoom.clan1 = 0
			currentRoom.clan2 = 0
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
	case 34:
		return s.voxelRoomList(c, r)
	case 32:
		c.send(65, &protocol.Writer{Data: []byte{32, 0, 0, 0, 0, 0}})
	case 33:
		c.send(65, &protocol.Writer{Data: []byte{33, 0, 0, 0, 0, 0}})
	case 35:
		return s.setZBAddons(c, r)
	case 37:
		return s.kickClanMembers(c)
	case 38:
		return s.roomNoticeClan(c)
	default:
		s.Log.Debug("unsupported room action", "kind", kind, "user", c.user.ID)
	}
	return r.Err
}

func (s *Server) leaveRoom(c *client) {
	r := s.rooms[c.room]
	if r == nil {
		return
	}
	if c.user.ID != 0 && s.users[c.user.ID] != nil && s.users[c.user.ID] != c {
		// The old connection lost its session to a reconnect. Remove its stale
		// membership silently; the new client adds itself when restoring.
		for i, u := range r.members {
			if u == c {
				r.members = append(r.members[:i], r.members[i+1:]...)
				break
			}
		}
		if r.match != nil {
			if stat := r.match.stats[c.user.ID]; stat != nil && stat.user == c {
				delete(r.match.stats, c.user.ID)
			}
		}
		c.room = 0
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
	if r.settings.number(70) != 0 {
		s.familyBattleRecompute(r)
	}
	s.refreshRooms()
}
