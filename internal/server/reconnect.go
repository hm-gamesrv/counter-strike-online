package server

import (
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

const reconnectGrace = 5 * time.Minute

type reconnectStat struct {
	kills        int
	deaths       int
	score        int
	class        int
	questMinutes int
	items        []store.Item
	started      time.Time
	questStreaks map[[3]int]int
}
type reconnectSlot struct {
	roomID  int
	matchID string
	expires time.Time
	team    int
	ready   int
	playing bool
	hasStat bool
	stat    reconnectStat
}

// stashReconnect remembers enough room and match state to put a dropped player
// back after logging in again. It must run before leaveRoom removes the member.
func (s *Server) stashReconnect(c *client) {
	if c.user.ID == 0 || c.room == 0 {
		return
	}
	if s.users[c.user.ID] != nil && s.users[c.user.ID] != c {
		// A newer session already owns this account.
		return
	}
	r := s.rooms[c.room]
	if r == nil || r.kicked[c.user.ID] {
		return
	}
	if r.dedicated != nil && r.dedicated.client == c {
		return
	}
	slot := reconnectSlot{
		roomID:  r.id,
		expires: time.Now().Add(reconnectGrace),
		team:    c.team,
		ready:   c.ready,
		playing: c.playing,
	}
	if r.match != nil {
		slot.matchID = r.match.id
		if stat := r.match.stats[c.user.ID]; stat != nil {
			slot.hasStat = true
			slot.stat = reconnectStat{
				kills:        stat.kills,
				deaths:       stat.deaths,
				score:        stat.score,
				class:        stat.class,
				questMinutes: stat.questMinutes,
				items:        append([]store.Item{}, stat.items...),
				started:      stat.started,
				questStreaks: map[[3]int]int{},
			}
			for key, value := range stat.questStreaks {
				slot.stat.questStreaks[key] = value
			}
		}
	}
	if s.restore == nil {
		s.restore = map[int]reconnectSlot{}
	}
	s.restore[c.user.ID] = slot
}

// restoreSession re-enters a waiting room or active match after a reconnect.
// The client still logs in through the normal lobby flow first; the room and
// game packets below switch it back.
func (s *Server) restoreSession(c *client) {
	slot, ok := s.restore[c.user.ID]
	if !ok {
		return
	}
	delete(s.restore, c.user.ID)
	if time.Now().After(slot.expires) {
		return
	}
	r := s.rooms[slot.roomID]
	if r == nil || r.kicked[c.user.ID] {
		return
	}
	if r.match != nil && r.match.id != slot.matchID {
		return
	}
	if r.dedicated != nil && slot.playing && r.dedicated.client == nil {
		return
	}
	if len(r.members) >= r.settings.number(8) {
		return
	}
	s.lobbyLeft(c)
	c.room = r.id
	c.team = slot.team
	c.ready = slot.ready
	if r.match != nil {
		c.playing = slot.playing
		if slot.hasStat {
			stat := &matchStat{
				user:         c,
				kills:        slot.stat.kills,
				deaths:       slot.stat.deaths,
				score:        slot.stat.score,
				class:        slot.stat.class,
				questMinutes: slot.stat.questMinutes,
				items:        append([]store.Item{}, slot.stat.items...),
				started:      slot.stat.started,
				questStreaks: map[[3]int]int{},
			}
			for key, value := range slot.stat.questStreaks {
				stat.questStreaks[key] = value
			}
			r.match.stats[c.user.ID] = stat
		}
	}
	w := &protocol.Writer{}
	w.U8(1)
	roomUser(w, c)
	for _, u := range r.members {
		u.send(65, w)
	}
	r.members = append(r.members, c)
	r.joined(c)
	if r.match != nil {
		w = &protocol.Writer{}
		w.Bytes([]byte{6, 1, byte(r.match.mode), 0})
		c.send(180, w)
		if slot.playing {
			s.joinGame(r, c)
		}
	}
	s.refreshRooms()
	if s.Log != nil {
		s.Log.Info("restored reconnect", "user", c.user.ID, "room", r.id, "playing", c.playing)
	}
}

func (s *Server) pruneRestore(now time.Time) {
	for id, slot := range s.restore {
		if now.After(slot.expires) {
			delete(s.restore, id)
		}
	}
}
