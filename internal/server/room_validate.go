package server

import (
	"encoding/binary"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// mapRow keeps the MapList.csv columns the room validator needs. Values holds
// every numeric column so the mode matrix and playlist rules can look up map
// support without a second parse.
type mapRow struct {
	MaxPlayer       int
	ZSMaxDifficulty int
	Values          map[string]int
}

func (m mapRow) value(name string) int {
	return m.Values[name]
}

func (m mapRow) valueWithPrefix(prefix string) int {
	for name, value := range m.Values {
		if strings.HasPrefix(name, prefix) {
			return value
		}
	}
	return 0
}

func loadMapRows(dir string) (map[int]mapRow, error) {
	f, err := os.Open(filepath.Join(dir, "Data", "MapList.csv"))
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
		columns[strings.TrimSpace(strings.TrimPrefix(name, ";"))] = i
	}
	for _, name := range []string{"map_id", "max_player"} {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("MapList.csv missing column %s", name)
		}
	}
	maps := map[int]mapRow{}
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(row) != len(header) {
			continue
		}
		entry := mapRow{Values: map[string]int{}}
		for name, index := range columns {
			value, _ := strconv.Atoi(strings.TrimSpace(row[index]))
			entry.Values[name] = value
		}
		id := entry.value("map_id")
		if id < 0 || id > 65535 {
			continue
		}
		entry.MaxPlayer = entry.value("max_player")
		if entry.MaxPlayer < 0 || entry.MaxPlayer > 32 {
			return nil, fmt.Errorf("MapList.csv: map %d has invalid max_player", id)
		}
		entry.ZSMaxDifficulty = entry.value("ZSmaxDifficulty")
		maps[id] = entry
	}
	if len(maps) == 0 {
		return nil, errors.New("MapList.csv has no maps")
	}
	return maps, nil
}

func funGameMode(mode int) bool {
	switch mode {
	case 10, 12, 16, 18, 19, 21, 25, 27, 31, 34, 37:
		return true
	}
	return false
}

func playroomGameMode(mode int) bool {
	switch mode {
	case 41, 48, 55:
		return true
	}
	return false
}

func voxelGameMode(mode int) bool {
	switch mode {
	case 38, 39, 49, 52, 53:
		return true
	}
	return false
}

// mapModeColumn mirrors CRoomSettings::GetGameModeNameByID.
func mapModeColumn(mode int) string {
	if funGameMode(mode) {
		return "map_FunMode"
	}
	if playroomGameMode(mode) {
		return "playroom"
	}
	switch mode {
	case 0:
		return "map_original"
	case 1:
		return "map_deathmode"
	case 2:
		return "map_teamdeathmode"
	case 3:
		return "map_bot_original"
	case 4:
		return "map_bot_deathmode"
	case 5:
		return "map_bot_teamdeathmod"
	case 6:
		return "official"
	case 7:
		return "official_tiebreak"
	case 8:
		return "map_zombi"
	case 9:
		return "map_zombi_expand"
	case 11:
		return "map_zombi_team"
	case 13:
		return "map_zombi_team_ann"
	case 14:
		return "map_zombi_3"
	case 15:
		return "map_zombie_survival"
	case 17:
		return "map_human_scenario"
	case 20:
		return "map_ZombieEscape"
	case 22:
		return "map_GDM"
	case 23:
		return "map_Basic"
	case 24:
		return "map_BZM"
	case 26:
		return "map_ZombieShelter"
	case 28:
		return "map_shelterteam"
	case 29:
		return "map_Zombie4"
	case 30:
		return "map_ZombieGiant"
	case 32:
		return "map_Zombie_Exterminate"
	case 33:
		return "map_standalone"
	case 35:
		return "Zombieofficial"
	case 36:
		return "Zombieofficial_tiebreak"
	case 40:
		return "allstar"
	case 42:
		return "season original"
	case 43:
		return "season zombie ex"
	case 44:
		return "season zombie hero"
	case 45:
		return "map_zombi_3z"
	case 46:
		return "map_zombietouchdown"
	case 47:
		return "season touchdown"
	case 49:
		return "prop_hunt"
	case 50:
		return "map_partner"
	case 51:
		return "map_zhe"
	case 52:
		return "vxlzshelter"
	case 53:
		return "scenariotx"
	case 54:
		return "zombi_5"
	case 56:
		return "zb_teamcontrol"
	case 57:
		return "map_tdm_supersoldier"
	case 58:
		return "map_zb3classic"
	case 59:
		return "ZS_Rift"
	}
	return ""
}

// mapAllowsMode mirrors CRoomSettings::IsMapValid.
func mapAllowsMode(mode int, row mapRow) bool {
	column := mapModeColumn(mode)
	if column == "" {
		return false
	}
	if row.value(column) == 0 {
		return false
	}
	if column == "map_FunMode" {
		return row.valueWithPrefix("Fun_Subtype") == mode
	}
	if column == "playroom" {
		return row.value("playroom_modeID") == mode
	}
	return true
}

func randomMapAllowed(mode int) bool {
	if playroomGameMode(mode) || voxelGameMode(mode) {
		return false
	}
	switch mode {
	case 15, 17, 30, 33, 50, 51:
		return false
	}
	return true
}

func mapPlaylistAllowed(mode int) bool {
	if funGameMode(mode) || playroomGameMode(mode) || voxelGameMode(mode) {
		return false
	}
	switch mode {
	case 26, 28, 33:
		return false
	}
	return true
}

func familyBattleAllowed(mode int) bool {
	switch mode {
	case 0, 2, 22, 32, 40, 56, 57:
		return true
	}
	return false
}

// playlist decodes the ROOM_LOWMID_MAPPLAYLIST block.
func (s *settings) playlist() []int {
	b := s.fields[41]
	if len(b) == 0 {
		return nil
	}
	size := int(b[0])
	if len(b) < 1+size*3 {
		return nil
	}
	ids := make([]int, 0, size)
	for i := 0; i < size; i++ {
		ids = append(ids, int(binary.LittleEndian.Uint16(b[1+i*3+1:])))
	}
	return ids
}

func (s *settings) zsDifficulty() int {
	b := s.fields[45]
	if len(b) == 0 {
		return 0
	}
	return int(b[0])
}

const (
	superRoomItemID = 8357
	bigHeadItemID   = 439
	c4SoundItemID   = 112

	// Settings field indexes for the three item-backed values. They match the
	// reference's ROOM_LOWMID_* bits: 58 is superRoom, 33 c4Timer and 44 sd.
	superRoomField = 58
	c4Field        = 33
	bigHeadField   = 44
)

// hostItemSetting pairs a settings field with the inventory item that backs it.
type hostItemSetting struct {
	Field int
	Item  int
}

// refreshHostRoomItems mirrors CRoom::CheckForHostItems and the item-backed
// settings in CRoomSettings::LoadNewSettings: superRoom, sd (big head event) and
// c4Timer come from items the host actually owns.
func (s *Server) refreshHostRoomItems(host *client, st *settings) error {
	// Both reference paths that consult items (LoadNewSettings/CheckNewSettings and
	// CheckForHostItems) use GetFirstActiveItemByItemID, whose query requires
	// status=1 AND inUse=1: merely owning a dormant copy grants nothing.
	active, err := func(id int) (bool, error) {
		return s.Store.HasActiveItem(host.user.ID, id)
	}(superRoomItemID)
	if err != nil {
		return err
	}
	st.setNumber(superRoomField, boolInt(active), 1)
	mode := st.number(6)
	if st.hasField(c4Field) && st.number(c4Field) != 0 { // C4 sound
		active, err = s.Store.HasActiveItem(host.user.ID, c4SoundItemID)
		if err != nil {
			return err
		}
		st.setNumber(c4Field, boolInt(active), 1)
	}
	switch mode {
	case 3, 4, 5, 15, 24:
		active, err = s.Store.HasActiveItem(host.user.ID, bigHeadItemID)
		if err != nil {
			return err
		}
		st.setNumber(bigHeadField, boolInt(active), 1)
	default:
		// CheckForHostItems writes the field with value 0 for modes that do not use
		// it; it does not drop the field.
		if st.hasField(bigHeadField) {
			st.setNumber(bigHeadField, 0, 1)
		}
	}
	return nil
}

// applyHostItems mirrors CRoom::CheckForHostItems on a freshly created room
// (src/room/room.cpp:899-930): superRoom and c4Timer always come from the host's
// active inventory items, and the big-head event (sd) is rewritten only in the
// modes that use it.
func (s *Server) applyHostItems(host *client, st *settings) error {
	active, err := s.Store.HasActiveItem(host.user.ID, superRoomItemID)
	if err != nil {
		return err
	}
	st.setNumber(superRoomField, boolInt(active), 1)
	active, err = s.Store.HasActiveItem(host.user.ID, c4SoundItemID)
	if err != nil {
		return err
	}
	st.setNumber(c4Field, boolInt(active), 1)
	switch st.number(6) {
	case 3, 4, 5, 15, 24:
		active, err = s.Store.HasActiveItem(host.user.ID, bigHeadItemID)
		if err != nil {
			return err
		}
		st.setNumber(bigHeadField, boolInt(active), 1)
	}
	return nil
}

// validateRoomSettings enforces the mode/map matrix, playlist and per-mode
// ranges when Room.ValidateSettings is enabled, then applies the cleanup the
// reference performs in CRoomSettings::LoadNewSettings on an accepted update.
func (s *Server) validateRoomSettings(st *settings, members int) error {
	return s.checkRoomSettings(st, members, true)
}

// validateNewRoomSettings is the create-time half of CRoomSettings::CheckSettings:
// the same matrix and range checks, without the LoadNewSettings cleanup. The
// reference never runs that cleanup when creating a room, and after
// LoadDefaultSettings every flag is set, so its "field present" gates would fire
// for fields the client never sent.
func (s *Server) validateNewRoomSettings(st *settings, members int) error {
	return s.checkRoomSettings(st, members, false)
}

func (s *Server) checkRoomSettings(st *settings, members int, sanitize bool) error {
	if !s.Config.Room.ValidateSettings {
		return nil
	}
	modeID := st.number(6)
	mode, ok := s.modes[modeID]
	if !ok {
		return errors.New("unsupported game mode")
	}
	capacity := st.number(8)
	if capacity < members || capacity > mode.MaxPlayers || mode.CreateMinPlayers > 0 && capacity < mode.CreateMinPlayers {
		return fmt.Errorf("room capacity %d outside mode range", capacity)
	}
	if !sanitize {
		// CheckSettings substitutes the mode defaults for a zero limit before it
		// validates them (src/room/roomsettings.cpp:2397-2413).
		if st.number(9) == 0 {
			st.setNumber(9, mode.Wins, 1)
		}
		if st.number(10) == 0 {
			st.setNumber(10, mode.Kills, 2)
		}
	}
	check := func(name string, value int, limit limitRange) error {
		if limit.Max == 0 {
			return nil
		}
		if value < limit.Min || value > limit.Max {
			return fmt.Errorf("%s %d outside %d..%d", name, value, limit.Min, limit.Max)
		}
		if limit.Step > 0 && (value-limit.Min)%limit.Step != 0 {
			return fmt.Errorf("%s %d is not a multiple of %d", name, value, limit.Step)
		}
		return nil
	}
	if err := check("win limit", st.number(9), mode.WinLimit); err != nil {
		return err
	}
	if err := check("kill limit", st.number(10), mode.KillLimit); err != nil {
		return err
	}
	if err := check("game time", st.number(11), mode.TimeLimit); err != nil {
		return err
	}
	if err := check("round time", st.number(12), mode.RoundTimeLimit); err != nil {
		return err
	}
	mapID := st.number(7)
	row, ok := s.maps[mapID]
	if !ok {
		return fmt.Errorf("unknown map %d", mapID)
	}
	if row.MaxPlayer > 0 && capacity > row.MaxPlayer {
		return fmt.Errorf("room capacity %d exceeds map limit %d", capacity, row.MaxPlayer)
	}
	if !mapAllowsMode(modeID, row) {
		return fmt.Errorf("map %d is not available for mode %d", mapID, modeID)
	}
	if st.flags[1]&(1<<8) != 0 {
		randomMap := st.number(40)
		if randomMap != 0 && !randomMapAllowed(modeID) {
			return errors.New("random map is not available for this mode")
		}
		if randomMap > 2 {
			st.setNumber(40, 2, 1)
		}
	}
	if st.flags[1]&(1<<9) != 0 {
		list := st.playlist()
		if len(list) < 2 || len(list) > 5 {
			return errors.New("map playlist must contain 2..5 maps")
		}
		if !mapPlaylistAllowed(modeID) {
			return errors.New("map playlist is not available for this mode")
		}
		seen := map[int]bool{}
		for _, id := range list {
			if seen[id] {
				return errors.New("map playlist contains duplicate maps")
			}
			seen[id] = true
			entry, ok := s.maps[id]
			if !ok {
				return fmt.Errorf("map playlist has unknown map %d", id)
			}
			if !mapAllowsMode(modeID, entry) {
				return fmt.Errorf("map playlist map %d is not available for mode %d", id, modeID)
			}
			if entry.value("weapon_restrict_deault") != 0 {
				return fmt.Errorf("map playlist map %d has a weapon restriction", id)
			}
		}
		if list[0] != mapID {
			return errors.New("room map is not the first playlist entry")
		}
	}
	if modeID == 15 {
		if st.flags[1]&(1<<13) == 0 {
			return errors.New("Zombie Scenario room needs a difficulty")
		}
		if st.zsDifficulty() > row.ZSMaxDifficulty {
			return fmt.Errorf("difficulty %d exceeds map limit %d", st.zsDifficulty(), row.ZSMaxDifficulty)
		}
	}
	if st.number(70) != 0 && !familyBattleAllowed(modeID) {
		return errors.New("family battle is not available for this mode")
	}
	if sanitize {
		s.sanitizeRoomSettings(st, modeID, mapID)
	}
	return nil
}
