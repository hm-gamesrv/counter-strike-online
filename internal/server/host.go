package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"sort"
	"strconv"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

type matchStat struct {
	user                                                                           *client
	kills, deaths, score, class                                                    int
	exp, points, bonusExp, bonusPoints, itemExp, itemPoints, classExp, classPoints int
	items                                                                          []store.Item
	started                                                                        time.Time
	questMinutes                                                                   int
	questStreaks                                                                   map[[3]int]int
}
type gameMatch struct {
	id                         string
	mode, mapID, ctWins, tWins int
	stats                      map[int]*matchStat
	saveData                   []byte
}

func (s *Server) startGame(c *client) error {
	r := s.rooms[c.room]
	if r == nil {
		return nil
	}
	w := &protocol.Writer{}
	w.Bytes([]byte{6, 1, byte(r.settings.number(6)), 0})
	c.send(180, w)
	if r.match != nil {
		if !c.playing {
			s.joinGame(r, c)
		}
		return nil
	}
	if r.host != c {
		c.notice("ROOM_JOIN_FAILED_CLOSED")
		return nil
	}
	if r.settings.number(70) != 0 && !s.familyBattleCanStart(r) {
		c.notice("ROOM_START_FAILED_CLAN_NOT_READY")
		return nil
	}
	var peer *dedicatedServer
	if s.Config.Room.HostConnectingMethod != 0 {
		peer = s.availableDedicated()
	}
	if s.Config.Room.HostConnectingMethod == 1 && peer == nil {
		c.notice("Cannot start game due to no available dedicated server")
		return nil
	}
	var id [16]byte
	if _, e := rand.Read(id[:]); e != nil {
		return e
	}
	r.match = &gameMatch{id: hex.EncodeToString(id[:]), mode: r.settings.number(6), mapID: r.settings.number(7), stats: map[int]*matchStat{}}
	actor, startID := c, c.user.ID
	if peer != nil {
		r.dedicated = peer
		peer.room = r.id
		peer.client.room = r.id
		r.joined(peer.client)
		actor = peer.client
		startID = peer.port
	}
	w = &protocol.Writer{}
	w.U8(0)
	w.U32(startID)
	w.U8(0)
	w.U8(0)
	w.U64(5555)
	actor.send(68, w)
	c.playing = true
	c.ready = 1
	s.roomStatus(r, true)
	for _, u := range r.members {
		if (u != c || peer != nil) && u.ready != 0 {
			s.joinGame(r, u)
		}
	}
	s.broadcastReady(r)
	s.refreshRooms()
	s.Log.Info("match started", "room", r.id, "match", r.match.id, "host", c.user.ID, "mode", r.match.mode, "map", r.match.mapID)
	return nil
}
func (s *Server) roomStatus(r *room, playing bool) {
	status, symbol := 0, 0
	if playing {
		status = 1
		symbol = 3
	}
	r.settings.flags[0] |= 1 << 30
	r.settings.fields[30] = []byte{byte(status)}
	r.settings.flags[1] |= 1 << 7
	r.settings.fields[39] = []byte{byte(symbol)}
	w := &protocol.Writer{}
	w.U8(4)
	w.U32(1 << 30)
	w.U32(1 << 7)
	w.U32(0)
	w.U32(0)
	w.U8(status)
	w.U8(symbol)
	r.broadcast(65, w)
}
func (s *Server) broadcastReady(r *room) {
	for _, u := range r.members {
		w := &protocol.Writer{}
		w.U8(3)
		w.U32(u.user.ID)
		w.U8(u.ready)
		r.broadcast(65, w)
	}
}
func hostNetwork(w *protocol.Writer, c *client) {
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
func (s *Server) joinGame(r *room, c *client) {
	c.playing = true
	c.ready = 1
	w := &protocol.Writer{}
	if r.dedicated != nil {
		w.U8(5)
		w.Bytes(r.dedicated.ip.To4())
		w.U16(r.dedicated.port)
		w.U64(int64(c.user.ID))
		c.send(68, w)
		s.broadcastReady(r)
		return
	}
	w.U8(1)
	w.U32(r.host.user.ID)
	w.U64(0)
	hostNetwork(w, r.host)
	c.send(68, w)
	s.broadcastReady(r)
}
func (s *Server) hostInventory(c *client, r *room, id int) error {
	u := s.users[id]
	if u == nil || u.room != r.id || !u.playing {
		return nil
	}
	owned, e := s.Store.Inventory(id)
	if e != nil {
		return e
	}
	var items []store.Item
	for _, id := range s.Config.DefaultUser.DefaultItems {
		items = append(items, store.Item{ID: id, Count: 1, Status: 1, InUse: 1})
	}
	items = append(items, owned...)
	active := items[:0]
	for _, item := range items {
		definition, ok := s.items[item.ID]
		weapon := definition.Category >= 1 && definition.Category <= 6 || definition.Category == 11
		if ok && definition.InGame && item.InUse != 0 && (!weapon || item.Status != 0) {
			active = append(active, item)
		}
	}
	w := &protocol.Writer{}
	w.U8(101)
	w.U32(id)
	w.U16(len(active))
	for _, item := range active {
		w.U16(item.ID)
		w.U8(0)
		w.U16(item.Count)
		w.U16(item.Paint)
		w.U16(item.Level)
		w.U32(item.Value)
		count := 0
		if item.Part1 != 0 {
			count++
		}
		if item.Part2 != 0 {
			count++
		}
		w.U8(count)
		if item.Part1 != 0 {
			w.U8(0)
			w.U16(item.Part1)
		}
		if item.Part2 != 0 {
			w.U8(1)
			w.U16(item.Part2)
		}
	}
	if len(w.Data) > protocol.MaxPayload {
		return errors.New("host inventory exceeds frame limit")
	}
	stat := r.match.stats[id]
	if stat == nil {
		stat = &matchStat{user: u, started: time.Now()}
		r.match.stats[id] = stat
	}
	stat.items = active
	c.send(68, w)
	return nil
}
func (s *Server) hostRequest(c *client, p *protocol.Reader) error {
	r := s.rooms[c.room]
	if r == nil || r.match == nil {
		return nil
	}
	if (r.dedicated == nil && r.host != c) || (r.dedicated != nil && r.dedicated.client != c) {
		return errors.New("host packet from non-host")
	}
	m := r.match
	kind := p.U8()
	switch kind {
	case 0, 18, 19, 24, 103: // Reference no-op/telemetry messages.
	case 1:
		n := p.U16()
		// Host migration adds its own header and up to 32 participant IDs.
		if n > protocol.MaxPayload-8-32*4 {
			return errors.New("host save data exceeds migration frame limit")
		}
		data := append([]byte{}, p.Bytes(n)...)
		if p.Err != nil {
			return p.Err
		}
		m.saveData = data
	case 101:
		id := p.U32()
		if p.Err != nil {
			return p.Err
		}
		return s.hostInventory(c, r, id)
	case 2:
		id, status := p.U32(), p.U8()
		if p.Err != nil {
			return p.Err
		}
		if status == 1 {
			u := s.users[id]
			if u != nil && u.room == r.id && m.mode == 15 {
				rows, e := s.Store.DB.Query("SELECT itemID FROM UserAddon WHERE userID=?", id)
				if e != nil {
					return e
				}
				var addons []int
				for rows.Next() {
					var item int
					if e = rows.Scan(&item); e != nil {
						rows.Close()
						return e
					}
					addons = append(addons, item)
				}
				e = rows.Err()
				rows.Close()
				if e != nil {
					return e
				}
				if len(addons) > 0 {
					w := &protocol.Writer{}
					w.U8(11)
					w.U32(id)
					w.U16(len(addons))
					for _, item := range addons {
						w.U16(item)
					}
					c.send(68, w)
				}
			}
		}
	case 3:
		killer := p.U32()
		gun, killerTeam := p.U16(), p.U8()
		victim := p.U32()
		victimTeam, killType := p.U8(), p.U16()
		p.Bytes(24)
		if p.Err != nil {
			return p.Err
		}
		if stat := m.stats[victim]; stat != nil {
			stat.questStreaks = nil
		}
		if stat := m.stats[killer]; stat != nil && killer != victim {
			stat.kills = min(stat.kills+1, 1000000)
			if err := s.applyQuestEvent(stat.user, m, stat, questEvent{Kind: 2, GunID: gun, KillerTeam: killerTeam, VictimTeam: victimTeam, VictimID: victim, VictimKillType: killType}); err != nil {
				return err
			}
		}
	case 5:
		if p.Err != nil {
			return p.Err
		}
		return s.endGame(r, false)
	case 7, 8:
		id := p.U32()
		p.U8()
		counter := p.I32()
		p.U8()
		if p.Err != nil {
			return p.Err
		}
		if counter < 0 || counter > 1000000 || kind == 8 && counter > 65535 {
			return errors.New("invalid match counter")
		}
		if stat := m.stats[id]; stat != nil {
			if kind == 7 {
				stat.kills = counter
			} else {
				if counter > stat.deaths {
					stat.questStreaks = nil
				}
				stat.deaths = counter
			}
		}
	case 9:
		t, ct := p.U8(), p.U8()
		p.Bytes(5)
		if p.Err != nil {
			return p.Err
		}
		m.ctWins = t
		m.tWins = ct // Preserve the upstream caller's CT/T argument order.
	case 10:
		id := p.U32()
		score := int(int8(p.U8()))
		p.U8()
		if p.Err != nil {
			return p.Err
		}
		if stat := m.stats[id]; stat != nil {
			stat.score += score
		}
	case 11:
		event, id := p.U8(), p.U32()
		value := 0
		switch event {
		case 32, 34:
			value = p.U32()
		case 77:
			p.U8()
		}
		if p.Err != nil {
			return p.Err
		}
		if stat := m.stats[id]; stat != nil {
			s.Log.Debug("match event", "room", r.id, "event", event, "user", id)
			if kind, ok := map[int]int{0: 6, 1: 7, 2: 8, 32: 10, 47: 11, 56: 12}[event]; ok {
				if err := s.applyQuestEvent(stat.user, m, stat, questEvent{Kind: kind, MonsterType: value}); err != nil {
					return err
				}
			}
			if event == 34 && value >= 3000 && value <= 3023 {
				s.grantDropBox(stat.user, value)
			}
		}
	case 13:
		p.U32()
		p.Bytes(8)
	case 14:
		id, class := p.U32(), p.U16()
		p.U8()
		if p.Err != nil {
			return p.Err
		}
		if stat := m.stats[id]; stat != nil {
			stat.class = class
		}
	case 16:
		p.U32()
		p.Bytes(12)
	case 17:
		p.U32()
		n := p.U8()
		p.Bytes(n * 8)
	case 20:
		p.Bytes(16)
		mapID := p.U16()
		if p.Err != nil {
			return p.Err
		}
		m.mapID = mapID
		w := &protocol.Writer{}
		w.U16(mapID)
		r.settings.flags[0] |= 1 << 7
		r.settings.fields[7] = w.Data
		r.settings.flags[1] |= 1 << 16
		r.settings.fields[48] = w.Data
		w = &protocol.Writer{}
		w.U8(4)
		w.U32(1 << 7)
		w.U32(1 << 16)
		w.U32(0)
		w.U32(0)
		w.U16(mapID)
		w.U16(mapID)
		r.broadcast(65, w)
	case 21:
		p.U32()
		if p.U8() == 1 {
			p.U8()
		}
	case 100:
		userID, itemID := p.U32(), p.U16()
		p.U16() // The reference implementation always consumes one item.
		if p.Err != nil {
			return p.Err
		}
		if p.Pos != len(p.Data) || itemID <= 0 {
			return errors.New("invalid scenario item request")
		}
		target := s.users[userID]
		if target == nil || target.room != r.id {
			return nil
		}
		protected := append([]int{}, s.Config.DefaultUser.DefaultItems...)
		protected = append(protected, s.Config.DefaultUser.PseudoDefaultItems...)
		changed, err := s.Store.ConsumeItem(target.user.ID, itemID, protected)
		if errors.Is(err, store.ErrItemUnavailable) || errors.Is(err, store.ErrItemProtected) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = s.sendInventoryItems(target, changed); err != nil {
			return err
		}
		w := &protocol.Writer{}
		w.U8(100)
		w.U32(userID)
		w.U16(itemID)
		w.U8(255)
		c.send(68, w)
	case 102:
		p.CString()
		if p.Err != nil {
			return p.Err
		}
		w := &protocol.Writer{}
		w.U8(102)
		typeID := s.Config.FlockingFlyerType
		name := "unknown"
		if typeID == 1 {
			name = "mosquito"
		} else if typeID == 2 {
			name = "kite"
		}
		w.String(name)
		if typeID != 0 {
			w.U8(1)
		} else {
			w.U8(0)
		}
		c.send(68, w)
	default:
		s.Log.Debug("unimplemented host message", "room", r.id, "kind", kind)
	}
	return p.Err
}
func (s *Server) endGame(r *room, forced bool) error {
	m := r.match
	if m == nil {
		return nil
	}
	stats := make([]*matchStat, 0, len(m.stats))
	for _, stat := range m.stats {
		stats = append(stats, stat)
	}
	sortMatchStats(stats, m.mode)
	coef, ok := s.Config.GameMatch.CalcResults.GameModeCoefficient[strconv.Itoa(m.mode)]
	if !ok {
		coef = s.Config.GameMatch.CalcResults.GameModeCoefficient["0"]
	}
	rewards := make([]store.MatchReward, 0, len(stats))
	for _, stat := range stats {
		stat.exp = matchBaseReward(coef.Exp, stat.kills)
		stat.points = matchBaseReward(coef.Points, stat.kills)
		stat.bonusExp = 0
		stat.bonusPoints = 0
		stat.classExp = 0
		stat.classPoints = 0
		classBonus := s.Config.GameMatch.CalcResults.BonusPercentage.Classes[strconv.Itoa(stat.class)]
		for _, item := range stat.items {
			if item.ID == stat.class && item.InUse != 0 && item.Status != 0 {
				stat.classExp = classBonus.Exp
				stat.classPoints = classBonus.Points
				break
			}
		}
		stat.bonusExp = stat.exp * stat.classExp / 100
		stat.bonusPoints = stat.points * stat.classPoints / 100
		s.applyEquipmentBonus(stat)
		update := m.mode == 0 || m.mode == 1 || m.mode == 2 || m.mode == 6 || m.mode == 22
		won := stat.user.team == 1 && m.tWins > m.ctWins || stat.user.team == 2 && m.ctWins > m.tWins
		rewards = append(rewards, store.MatchReward{UserID: stat.user.user.ID, Exp: stat.exp + stat.bonusExp, Points: stat.points + stat.bonusPoints, Kills: stat.kills, Deaths: stat.deaths, UpdateStats: update, QuestMatchEnd: !forced, Won: won, Mode: m.mode, Map: m.mapID, Players: len(stats)})
	}
	if e := s.Store.SettleMatch(m.id, rewards); e != nil {
		return e
	}
	for _, stat := range stats {
		ch, e := s.Store.Character(stat.user.user.ID)
		if e != nil {
			return e
		}
		if err := s.drainQuestEvents(stat.user); err != nil {
			return err
		}
		stat.user.character = ch
		w := &protocol.Writer{}
		w.U32(ch.ID)
		if forced || !stat.user.playing {
			userInfo(w, ch)
		} else {
			// The result UI adds this match's points and counters to its
			// cached totals. Publish those totals when the result is closed.
			w.U32(1<<3 | 1<<5)
			w.U32(0)
			w.U32(ch.Level)
			w.U64(int64(ch.Exp))
		}
		stat.user.send(157, w)
	}
	s.roomStatus(r, false)
	result := matchResult(m, stats)
	for _, u := range r.members {
		if u.playing {
			u.send(68, &protocol.Writer{Data: []byte{3}})
			if !forced {
				u.send(65, result)
			}
		}
		u.playing = false
		u.ready = 0
	}
	r.match = nil
	rotated := s.rotateRoomMap(r, forced)
	if !rotated || s.Config.Room.HostConnectingMethod != 1 || r.settings.number(42) == 1 {
		releaseDedicated(r)
	}
	s.broadcastReady(r)
	s.refreshRooms()
	s.Log.Info("match settled", "room", r.id, "match", m.id, "participants", len(stats), "forced", forced)
	return nil
}
func matchResult(m *gameMatch, stats []*matchStat) *protocol.Writer {
	w := &protocol.Writer{}
	w.U8(6)
	winner := 1
	if m.ctWins > m.tWins {
		winner = 2
	}
	w.U8(winner)
	w.U8(0)
	w.U8(0)
	w.U8(len(stats))
	w.U8(m.mode)
	for i, stat := range stats {
		w.U32(stat.user.user.ID)
		w.U8(stat.user.team)
		w.U32(stat.kills)
		w.U16(stat.deaths)
		w.U8(0)
		w.U8(0)
		w.U16(stat.exp)
		w.U32(stat.bonusExp)
		w.U16(stat.points)
		w.U32(stat.bonusPoints)
		if i == 0 {
			w.U8(1)
		} else {
			w.U8(0)
		}
		w.U8(0)
		for _, v := range []int{0, stat.itemPoints, stat.classPoints, 0, 0, 0, 0, 0, 0, stat.itemExp, stat.classExp, 0, 0, 0, 0, 0, 0, 0} {
			w.U16(v)
		}
		w.Bytes(make([]byte, 4))
		extras := map[int]int{0: 5, 3: 5, 6: 5, 7: 5, 50: 5, 8: 4, 9: 4, 14: 4, 20: 4, 24: 4, 32: 4, 35: 4, 36: 4, 45: 4, 54: 4, 10: 4, 11: 8, 13: 8, 12: 6, 15: 10, 17: 10, 16: 24, 18: 10, 21: 3, 23: 7, 25: 6, 26: 7, 27: 8, 28: 28, 52: 28, 29: 12, 30: 12, 33: 6, 37: 9, 38: 4, 39: 4, 42: 16, 43: 16, 44: 16, 46: 2, 47: 15, 49: 4, 51: 2, 56: 14}
		w.Bytes(make([]byte, extras[m.mode]))
		w.U8(0)
	}
	return w
}

func matchBaseReward(coefficient, kills int) int {
	if coefficient <= 0 {
		return 0
	}
	kills = max(kills, 1)
	if coefficient >= 9999 || kills > 9999/coefficient {
		return 9999
	}
	return coefficient * kills
}
func (s *Server) migrateHost(r *room) {
	if r.match == nil || r.dedicated != nil {
		return
	}
	if !r.host.playing {
		if e := s.endGame(r, true); e != nil {
			s.Log.Error("forced match settlement failed", "error", e)
		}
		return
	}
	for _, u := range r.members {
		if !u.playing {
			continue
		}
		w := &protocol.Writer{}
		w.U8(2)
		w.U32(r.host.user.ID)
		if u == r.host {
			w.U16(len(r.match.saveData))
			w.Bytes(r.match.saveData)
			w.U8(len(r.match.stats))
			ids := make([]int, 0, len(r.match.stats))
			for id := range r.match.stats {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			for _, id := range ids {
				w.U32(id)
			}
		} else {
			w.U8(0)
		}
		u.send(68, w)
		if u != r.host {
			s.joinGame(r, u)
		}
	}
}

// grantDropBox awards the scene drop box prize carried by host game event 34.
func (s *Server) grantDropBox(c *client, rewardID int) {
	if err := s.grantCatalogReward(c, rewardID); err != nil && s.Log != nil {
		s.Log.Warn("drop box grant failed", "user", c.user.ID, "reward", rewardID, "error", err)
	}
}

// applyEquipmentBonus adds the configured equipment percentage for active
// items carried into the match. Upstream had this loop commented out while the
// configuration and result fields already existed.
func (s *Server) applyEquipmentBonus(stat *matchStat) {
	for key, bonus := range s.Config.GameMatch.CalcResults.BonusPercentage.Items {
		itemID, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		for _, item := range stat.items {
			if item.ID != itemID || item.InUse == 0 || item.Status == 0 {
				continue
			}
			stat.itemExp = bonus.Exp
			stat.itemPoints = bonus.Points
			stat.bonusExp += stat.exp * bonus.Exp / 100
			stat.bonusPoints += stat.points * bonus.Points / 100
			break
		}
	}
}

// Individual deathmatch modes rank by score before kills; round-based modes
// keep the upstream kill ordering.
func sortMatchStats(stats []*matchStat, mode int) {
	individual := mode == 1 || mode == 4 || mode == 22
	sort.Slice(stats, func(i, j int) bool {
		if individual && stats[i].score != stats[j].score {
			return stats[i].score > stats[j].score
		}
		if stats[i].kills == stats[j].kills {
			return stats[i].user.user.ID < stats[j].user.user.ID
		}
		return stats[i].kills > stats[j].kills
	})
}
