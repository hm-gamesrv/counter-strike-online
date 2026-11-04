package server

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

type limitRange struct {
	Min  int
	Max  int
	Step int
}
type modeDefinition struct {
	MaxPlayers       int
	MinPlayers       int
	CreateMinPlayers int
	Wins             int
	Kills            int
	GameTime         int
	RoundTime        int
	WinLimit         limitRange
	KillLimit        limitRange
	TimeLimit        limitRange
	RoundTimeLimit   limitRange
}

// The mode table shipped with the selected client is authoritative. Each limit
// is encoded as minimum|maximum|default|step, rather than as a single integer.
func loadModes(dir string) (map[int]modeDefinition, error) {
	f, err := os.Open(filepath.Join(dir, "Data", "GameModeList.csv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	columns := map[string]int{}
	for i, name := range header {
		columns[strings.TrimPrefix(strings.TrimSpace(name), ";")] = i
	}
	for _, name := range []string{"mode_id", "mode_maxplayer", "mode_win_limit_id", "mode_kill_limit_id", "mode_time_limit_id", "mode_roundtime_id"} {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("GameModeList.csv missing column %s", name)
		}
	}
	modes := map[int]modeDefinition{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		value := func(name string) int {
			index := columns[name]
			if index >= len(row) {
				return 0
			}
			v, _ := strconv.Atoi(strings.TrimSpace(row[index]))
			return v
		}
		rangeValue := func(name string) limitRange {
			index := columns[name]
			if index >= len(row) {
				return limitRange{}
			}
			parts := strings.Split(row[index], "|")
			if len(parts) != 4 {
				return limitRange{}
			}
			value := func(part string) int {
				v, _ := strconv.Atoi(strings.TrimSpace(part))
				return v
			}
			r := limitRange{Min: value(parts[0]), Max: value(parts[1]), Step: value(parts[3])}
			if r.Max < r.Min || r.Min < 0 || r.Step < 0 {
				return limitRange{}
			}
			return r
		}
		defaultValue := func(name string) int {
			index := columns[name]
			if index >= len(row) {
				return 0
			}
			parts := strings.Split(row[index], "|")
			if len(parts) != 4 {
				return 0
			}
			v, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
			return v
		}
		id := value("mode_id")
		if id < 0 || value("mode_maxplayer") == 0 {
			continue
		}
		modes[id] = modeDefinition{
			MaxPlayers:       value("mode_maxplayer"),
			MinPlayers:       value("mode_minplayer"),
			CreateMinPlayers: value("mode_create_minplayer"),
			Wins:             defaultValue("mode_win_limit_id"),
			Kills:            defaultValue("mode_kill_limit_id"),
			GameTime:         defaultValue("mode_time_limit_id"),
			RoundTime:        defaultValue("mode_roundtime_id"),
			WinLimit:         rangeValue("mode_win_limit_id"),
			KillLimit:        rangeValue("mode_kill_limit_id"),
			TimeLimit:        rangeValue("mode_time_limit_id"),
			RoundTimeLimit:   rangeValue("mode_roundtime_id"),
		}
	}
	if len(modes) == 0 {
		return nil, fmt.Errorf("GameModeList.csv has no modes")
	}
	return modes, nil
}

// applyMapDefaults mirrors the ROOM_LOW_MAPID block of CRoomSettings::LoadNewSettings.
// The reference runs it on every map change, outside the validateSettings gate,
// because these values come from the map table rather than from the client. It
// returns the fields it wrote so they can join the broadcast mask.
func (s *Server) applyMapDefaults(st *settings, mapID int) settings {
	var added settings
	mark := func(index int) {
		added.flags[index/32] |= 1 << uint(index%32)
	}
	row, known := s.maps[mapID]
	if mapID != 254 && known {
		st.setNumber(8, row.MaxPlayer, 1) // maxPlayers follows the map
		mark(8)
	}
	if known {
		if restriction := row.value("weapon_restrict_deault"); restriction != 0 {
			st.setNumber(69, restriction, 1)
			mark(69)
		}
		st.setNumber(38, row.value("ball_default"), 1)
		mark(38)
	}
	st.setNumber(59, boolInt(mapID == 282), 1)
	mark(59)
	// The reference always resends the playlist and the displayed map so the
	// client repaints; the playlist value itself is whatever the room already has.
	if st.fields[41] != nil {
		mark(41)
	}
	st.setNumber(48, mapID, 2)
	mark(48)
	return added
}

func (s *settings) setNumber(index, value, width int) {
	w := &protocol.Writer{}
	if width == 2 {
		w.U16(value)
	} else {
		w.U8(value)
	}
	s.flags[index/32] |= 1 << (index % 32)
	s.fields[index] = w.Data
}

// Mirrors the common gameplay defaults in CRoomSettings::LoadDefaultSettings.
// Creation keeps the requested capacity and nonzero win/kill limits and then
// runs the full LoadDefaultSettings pass; changing the mode resets those values,
// as in the upstream update path.
func (s *Server) roomDefaults(st *settings, creating bool) bool {
	mode := st.number(6)
	d, ok := s.modes[mode]
	if !ok {
		return false
	}
	if creating {
		s.loadDefaultSettings(st, mode)
		return true
	}
	st.setNumber(8, d.MaxPlayers, 1)
	st.setNumber(9, d.Wins, 1)
	st.setNumber(10, d.Kills, 2)
	st.setNumber(11, d.GameTime, 1)
	st.setNumber(12, d.RoundTime, 1)
	st.setNumber(13, defaultWeaponLimit(mode), 1)
	st.setNumber(14, 0, 1)
	st.setNumber(15, 0, 1)
	st.setNumber(16, defaultBuyTime(mode), 1)
	st.setNumber(17, 1, 1)
	st.setNumber(18, defaultTeamBalance(mode), 1)
	st.setNumber(20, defaultFriendlyFire(mode), 1)
	st.setNumber(21, 1, 1)
	st.setNumber(22, 1, 1)
	st.setNumber(28, defaultViewFlag(mode), 1)
	st.setNumber(29, 1, 1)
	friendly, enemy, add := defaultFriendlyBots(mode), defaultEnemyBots(mode), defaultBotAdd(mode)
	st.flags[1] |= 1 << 2
	st.fields[34] = []byte{0, byte(friendly), byte(enemy), byte(add), byte(add)}
	st.setNumber(36, defaultStartingCash(mode), 2)
	if st.fields[7] != nil {
		st.setNumber(48, st.number(7), 2)
	}
	return true
}

func defaultWeaponLimit(mode int) int {
	if mode == 2 || mode == 5 || mode == 23 {
		return 9
	}
	return 0
}

func defaultBuyTime(mode int) int {
	switch mode {
	case 0:
		return 20
	case 32:
		return 17
	case 45:
		return 30
	case 50:
		return 40
	case 1, 4, 19, 22, 27, 40, 57:
		return 60
	}
	return 90
}

func defaultViewFlag(mode int) int {
	switch mode {
	case 15, 17, 28, 33:
		return 32
	case 0, 32, 37, 50:
		return 1 | 1<<5 | 1<<7
	}
	return 0
}

func defaultStartingCash(mode int) int {
	switch mode {
	case 0, 3, 28, 52:
		return 800
	case 50:
		return 5000
	case 15, 17:
		return 7500
	case 45:
		return 8000
	}
	return 0
}

func defaultGameRule(mode int) int {
	if mode == 40 {
		return 1
	}
	return 0
}

func defaultZbRebalance(mode int) int {
	if mode == 14 {
		return 1
	}
	return 0
}

func defaultFloatingDamageSkin(mode int) int {
	if mode == 0 {
		return 0
	}
	return 1
}

func defaultPlayerOneTeam(mode int) int {
	if mode == 3 || mode == 5 {
		return 1
	}
	return 0
}

func defaultMutationLimit(mode int) int {
	if mode == 45 {
		return 40
	}
	return 0
}

func defaultEnhanceRestrict(mode int) int {
	if mode == 22 || mode == 32 {
		return 1
	}
	return 0
}

// setBytes stores a whole field payload and marks its flag bit.
func (s *settings) setBytes(index int, data []byte) {
	s.flags[index/32] |= 1 << uint(index%32)
	s.fields[index] = data
}

// loadDefaultSettings mirrors CRoomSettings::LoadDefaultSettings
// (src/room/roomsettings.cpp:1210-1321). The reference runs it at the end of
// CheckSettings, after the create request has been validated, so a created room
// carries ROOM_*_ALL flag words and a complete field set in its CreateAndJoin
// frame. Fields the reference leaves alone (room name, password, mode, map,
// capacity, win/kill limits, random map, playlist, family battle, superRoom and
// sd) keep whatever the request carried.
func (s *Server) loadDefaultSettings(st *settings, mode int) {
	const allFlags = 0xFFFFFFFF
	st.flags = [4]uint32{
		allFlags,
		allFlags,
		allFlags,
		allFlags,
	}
	mapID := st.number(7)
	if mapID != 254 {
		st.flags[1] &^= 1 << 18
	}
	// Values the reference keeps from the parsed request need a payload even when
	// the client left the field out: ROOM_*_ALL always announces them.
	for index, width := range map[int]int{
		0:  1, // room name, replaced below by the parsed string
		3:  1, // password
		6:  1, // game mode
		7:  2, // map
		8:  1, // max players
		40: 1, // random map
		41: 1, // map playlist, empty
		44: 1, // sd (big head event)
		58: 1, // super room
		70: 1, // family battle
	} {
		if st.fields[index] == nil {
			st.fields[index] = make([]byte, width)
		}
	}
	d := s.modes[mode]
	row := s.maps[mapID]
	st.setNumber(1, 0, 1)
	st.setBytes(2, make([]byte, 7))
	st.setNumber(4, 0, 1)
	st.setNumber(5, 0, 1)
	st.setNumber(11, d.GameTime, 1)
	st.setNumber(12, d.RoundTime, 1)
	st.setNumber(13, defaultWeaponLimit(mode), 1)
	st.setNumber(14, 0, 1)
	st.setNumber(15, 0, 1)
	st.setNumber(16, defaultBuyTime(mode), 1)
	st.setNumber(17, 1, 1)
	st.setNumber(18, defaultTeamBalance(mode), 1)
	st.setNumber(19, 0, 1)
	st.setNumber(20, defaultFriendlyFire(mode), 1)
	st.setNumber(21, 1, 1)
	st.setNumber(22, 1, 1)
	st.setNumber(23, 0, 1)
	st.setNumber(24, 0, 1)
	st.setNumber(25, 0, 1)
	st.setNumber(26, 0, 1)
	st.setNumber(27, 0, 1)
	st.setNumber(28, defaultViewFlag(mode), 1)
	st.setNumber(29, 1, 1)
	st.setNumber(30, 0, 1)
	// unk33 followed by the two zero-filled entries WriteSettings always emits.
	st.setBytes(31, make([]byte, 1+2*17))
	// unk34, the empty unk35 string and unk36..unk38.
	st.setBytes(32, make([]byte, 8))
	st.setNumber(33, 0, 1)
	st.setBytes(34, []byte{0, byte(defaultFriendlyBots(mode)), byte(defaultEnemyBots(mode)), byte(defaultBotAdd(mode)), byte(defaultBotAdd(mode))})
	st.setNumber(35, 0, 1)
	st.setNumber(36, defaultStartingCash(mode), 2)
	st.setNumber(37, 0, 1)
	st.setNumber(38, row.value("ball_default"), 1)
	st.setNumber(39, 0, 1)
	playlistIndex := 0
	if len(st.fields[41]) != 0 && st.fields[41][0] != 0 {
		playlistIndex = 1
	}
	st.setNumber(42, playlistIndex, 1)
	st.setNumber(43, defaultEnhanceRestrict(mode), 1)
	if mode != 15 {
		st.setBytes(45, make([]byte, 9))
	} else if len(st.fields[45]) != 9 {
		field := make([]byte, 9)
		copy(field, st.fields[45])
		st.setBytes(45, field)
	}
	st.setNumber(46, 0, 1)
	st.setNumber(47, 0, 1)
	st.setNumber(48, mapID, 2)
	// zbLimitFlag plus the four empty limit slots.
	st.setBytes(49, make([]byte, 17))
	st.setNumber(52, 0, 1)
	st.setNumber(53, 0, 1)
	st.setNumber(54, 0, 1)
	st.setNumber(55, boolInt(zbRespawnDefault(mode)), 1)
	st.setNumber(56, boolInt(zbBalanceDefault(mode)), 1)
	st.setNumber(57, defaultGameRule(mode), 1)
	isZbCompetitive := 0
	if mapID == 282 {
		isZbCompetitive = 1
	}
	st.setNumber(59, isZbCompetitive, 1)
	st.setNumber(60, 0, 1)
	st.setNumber(61, 0, 1)
	st.setNumber(62, 0, 1)
	st.setNumber(64, 1, 1)
	// mutationRestrict and the four "unset" entries WriteSettings falls back to.
	st.setBytes(65, []byte{0, 0xFF, 0xFF, 0xFF, 0xFF})
	st.setNumber(66, defaultMutationLimit(mode), 1)
	st.setNumber(67, defaultFloatingDamageSkin(mode), 1)
	st.setNumber(68, defaultPlayerOneTeam(mode), 1)
	st.setNumber(69, row.value("weapon_restrict_deault"), 1)
	if st.number(9) == 0 {
		st.setNumber(9, d.Wins, 1)
	}
	if st.number(10) == 0 {
		st.setNumber(10, d.Kills, 2)
	}
	if isZbCompetitive != 0 {
		st.setNumber(8, 32, 1)
		win := 13
		if mode == 14 {
			win = 7
		}
		st.setNumber(9, win, 1)
		st.setNumber(12, 3, 1)
	}
	if st.number(70) != 0 {
		applyFamilyBattleDefaults(st, mode)
	}
	st.setNumber(71, 0, 1)
	st.setNumber(72, defaultZbRebalance(mode), 1)
	// unk79_1, unk79_2, unk79_3 and unk79_4.
	st.setBytes(73, make([]byte, 10))
	st.setNumber(127, 0, 1)
}

// applyFamilyBattleDefaults mirrors CRoomSettings::LoadFamilyBattleSettings
// (src/room/roomsettings.cpp:1063-1208), which LoadDefaultSettings applies when
// the created room is a Family Battle.
func applyFamilyBattleDefaults(st *settings, mode int) {
	st.setNumber(8, 10, 1)
	st.setNumber(11, 150, 1)
	st.setNumber(13, 0, 1)
	st.setNumber(14, 0, 1)
	st.setNumber(15, 0, 1)
	st.setNumber(16, 20, 1)
	st.setNumber(17, 1, 1)
	st.setNumber(18, 0, 1)
	st.setNumber(20, 0, 1)
	st.setNumber(21, 1, 1)
	st.setNumber(28, 1|1<<5|1<<7, 1)
	st.setNumber(29, 1, 1)
	st.setNumber(35, 0, 1)
	st.setNumber(47, 0, 1)
	st.setNumber(69, 0, 1)
	switch mode {
	case 0:
		st.setNumber(9, 8, 1)
		st.setNumber(12, 2, 1)
		st.setNumber(13, 2, 1)
		st.setNumber(36, 800, 2)
		st.setNumber(43, 1, 1)
		st.setNumber(46, 0, 1)
		st.setNumber(54, 1, 1)
		st.setNumber(67, 0, 1)
	case 2:
		st.setNumber(10, 80, 2)
		st.setNumber(13, 9, 1)
		st.setNumber(61, 1, 1)
		st.setNumber(67, 0, 1)
	case 32:
		st.setNumber(9, 5, 1)
		st.setNumber(12, 2, 1)
		st.setNumber(46, 0, 1)
		st.setNumber(55, 0, 1)
		st.setNumber(56, 0, 1)
	case 40:
		st.setNumber(10, 120, 2)
		st.setNumber(57, 0, 1)
	case 56:
		st.setNumber(16, 60, 1)
		st.setNumber(56, 0, 1)
		st.setNumber(61, 1, 1)
	case 57:
		st.setNumber(10, 120, 2)
		st.setNumber(16, 60, 1)
		st.setNumber(61, 1, 1)
	}
}
