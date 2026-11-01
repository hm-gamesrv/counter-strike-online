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

type modeDefinition struct {
	maxPlayers, wins, kills, gameTime, roundTime int
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
		value := func(name string, limit bool) int {
			index := columns[name]
			if index >= len(row) {
				return 0
			}
			cell := row[index]
			if limit {
				parts := strings.Split(cell, "|")
				if len(parts) != 4 {
					return 0
				}
				cell = parts[2]
			}
			v, _ := strconv.Atoi(strings.TrimSpace(cell))
			return v
		}
		id := value("mode_id", false)
		if id < 0 || value("mode_maxplayer", false) == 0 {
			continue
		}
		modes[id] = modeDefinition{value("mode_maxplayer", false), value("mode_win_limit_id", true), value("mode_kill_limit_id", true), value("mode_time_limit_id", true), value("mode_roundtime_id", true)}
	}
	if len(modes) == 0 {
		return nil, fmt.Errorf("GameModeList.csv has no modes")
	}
	return modes, nil
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
// Creation retains requested capacity and nonzero win/kill limits; changing the
// mode resets those values, as in the upstream update path.
func (s *Server) roomDefaults(st *settings, creating bool) bool {
	mode := st.number(6)
	d, ok := s.modes[mode]
	if !ok {
		return false
	}
	if !creating {
		st.setNumber(8, d.maxPlayers, 1)
	}
	if !creating || st.number(9) == 0 {
		st.setNumber(9, d.wins, 1)
	}
	if !creating || st.number(10) == 0 {
		st.setNumber(10, d.kills, 2)
	}
	st.setNumber(11, d.gameTime, 1)
	st.setNumber(12, d.roundTime, 1)
	weapon := 0
	if mode == 2 || mode == 5 || mode == 23 {
		weapon = 9
	}
	st.setNumber(13, weapon, 1)
	buyTime := 90
	switch mode {
	case 0:
		buyTime = 20
	case 32:
		buyTime = 17
	case 45:
		buyTime = 30
	case 50:
		buyTime = 40
	case 1, 4, 19, 22, 27, 40, 57:
		buyTime = 60
	}
	st.setNumber(14, 0, 1)
	st.setNumber(15, 0, 1)
	st.setNumber(16, buyTime, 1)
	st.setNumber(17, 1, 1)
	balance := 1
	switch mode {
	case 41, 48, 50, 55, 56:
		balance = 0
	}
	st.setNumber(18, balance, 1)
	friendlyFire := 0
	switch mode {
	case 1, 4, 38, 39:
		friendlyFire = 1
	}
	st.setNumber(20, friendlyFire, 1)
	st.setNumber(21, 1, 1)
	st.setNumber(22, 1, 1)
	view := 0
	switch mode {
	case 15, 17, 28, 33:
		view = 32
	case 0, 32, 37, 50:
		view = 161
	}
	st.setNumber(28, view, 1)
	st.setNumber(29, 1, 1)
	friendly, enemy, add := 0, 0, 0
	switch mode {
	case 3, 4, 5:
		friendly, enemy, add = 4, 5, 1
	case 24, 57:
		friendly, enemy, add = 5, 6, 1
	case 22:
		friendly, enemy, add = 7, 8, 1
	case 14, 45, 54:
		friendly, enemy, add = 8, 8, 1
	}
	st.flags[1] |= 1 << 2
	st.fields[34] = []byte{0, byte(friendly), byte(enemy), byte(add), byte(add)}
	cash := 0
	switch mode {
	case 0, 3, 28, 52:
		cash = 800
	case 50:
		cash = 5000
	case 15, 17:
		cash = 7500
	case 45:
		cash = 8000
	}
	st.setNumber(36, cash, 2)
	if creating || st.fields[7] != nil {
		st.setNumber(48, st.number(7), 2)
	}
	return true
}
