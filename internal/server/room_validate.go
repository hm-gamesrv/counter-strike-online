package server

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func loadMapLimits(dir string) (map[int]int, error) {
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
	maps := map[int]int{}
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
		id := value("map_id")
		if id < 0 || id > 65535 {
			continue
		}
		max := value("max_player")
		if max < 0 || max > 32 {
			return nil, fmt.Errorf("MapList.csv: map %d has invalid max_player", id)
		}
		maps[id] = max
	}
	if len(maps) == 0 {
		return nil, errors.New("MapList.csv has no maps")
	}
	return maps, nil
}

// validateRoomSettings enforces the per-mode ranges from GameModeList.csv and
// the map player cap when Room.ValidateSettings is enabled.
func (s *Server) validateRoomSettings(st *settings, members int) error {
	if !s.Config.Room.ValidateSettings {
		return nil
	}
	mode, ok := s.modes[st.number(6)]
	if !ok {
		return errors.New("unsupported game mode")
	}
	capacity := st.number(8)
	if capacity < members || capacity > mode.MaxPlayers || mode.CreateMinPlayers > 0 && capacity < mode.CreateMinPlayers {
		return fmt.Errorf("room capacity %d outside mode range", capacity)
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
	mapMax, ok := s.mapMax[mapID]
	if !ok {
		return fmt.Errorf("unknown map %d", mapID)
	}
	if mapMax > 0 && capacity > mapMax {
		return fmt.Errorf("room capacity %d exceeds map limit %d", capacity, mapMax)
	}
	return nil
}
