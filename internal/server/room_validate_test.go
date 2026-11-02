package server

import (
	"encoding/binary"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func validRoomSettings() settings {
	st := settings{}
	st.setNumber(6, 0, 1)
	st.setNumber(7, 1, 2)
	st.setNumber(8, 8, 1)
	st.setNumber(9, 9, 1)
	st.setNumber(10, 40, 2)
	st.setNumber(11, 150, 1)
	st.setNumber(12, 2, 1)
	return st
}

func validateServer() *Server {
	s := &Server{
		Config: config.Config{},
		modes: map[int]modeDefinition{
			0: {
				MaxPlayers:       32,
				CreateMinPlayers: 2,
				WinLimit:         limitRange{Min: 5, Max: 33, Step: 4},
				KillLimit:        limitRange{Min: 20, Max: 120, Step: 20},
				TimeLimit:        limitRange{Min: 150, Max: 150},
				RoundTimeLimit:   limitRange{Min: 2, Max: 3, Step: 1},
			},
			15: {
				MaxPlayers:       8,
				CreateMinPlayers: 1,
			},
		},
		maps: map[int]mapRow{
			1: {
				MaxPlayer: 20,
				Values:    map[string]int{"map_id": 1, "map_original": 1},
			},
			2: {
				MaxPlayer: 20,
				Values:    map[string]int{"map_id": 2, "map_original": 1, "weapon_restrict_deault": 1},
			},
			3: {
				MaxPlayer:       16,
				ZSMaxDifficulty: 2,
				Values:          map[string]int{"map_id": 3, "map_zombie_survival": 1},
			},
			4: {
				MaxPlayer: 16,
				Values:    map[string]int{"map_id": 4, "map_original": 1},
			},
		},
	}
	s.Config.Room.ValidateSettings = true
	return s
}

func TestValidateRoomSettings(t *testing.T) {
	s := validateServer()
	st := validRoomSettings()
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}

	for name, mutate := range map[string]func(*settings){
		"win below minimum":  func(st *settings) { st.setNumber(9, 4, 1) },
		"win above maximum":  func(st *settings) { st.setNumber(9, 34, 1) },
		"win off step":       func(st *settings) { st.setNumber(9, 10, 1) },
		"kill off step":      func(st *settings) { st.setNumber(10, 41, 2) },
		"time outside":       func(st *settings) { st.setNumber(11, 149, 1) },
		"round above":        func(st *settings) { st.setNumber(12, 4, 1) },
		"below create min":   func(st *settings) { st.setNumber(8, 1, 1) },
		"above mode maximum": func(st *settings) { st.setNumber(8, 33, 1) },
		"above map maximum":  func(st *settings) { st.setNumber(8, 21, 1) },
		"unknown map":        func(st *settings) { st.setNumber(7, 99, 2) },
		"map without mode":   func(st *settings) { st.setNumber(7, 3, 2) },
	} {
		invalid := validRoomSettings()
		mutate(&invalid)
		if err := s.validateRoomSettings(&invalid, 1); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}

	// Current members still have to fit even if the configured minimum is lower.
	if err := s.validateRoomSettings(&st, 9); err == nil {
		t.Fatal("expected capacity below members to fail")
	}

	s.Config.Room.ValidateSettings = false
	invalid := validRoomSettings()
	invalid.setNumber(8, 1, 1)
	invalid.setNumber(9, 100, 1)
	if err := s.validateRoomSettings(&invalid, 1); err != nil {
		t.Fatalf("validation disabled: %v", err)
	}
}

func playlistSettings(ids ...int) settings {
	st := validRoomSettings()
	b := []byte{byte(len(ids))}
	for _, id := range ids {
		b = append(b, 0, byte(id), byte(id>>8))
	}
	st.fields[41] = b
	st.flags[1] |= 1 << 9
	return st
}

func TestValidateRoomPlaylist(t *testing.T) {
	s := validateServer()
	if err := s.validateRoomSettings(ptr(playlistSettings(1, 4)), 1); err != nil {
		t.Fatalf("valid playlist: %v", err)
	}
	for name, st := range map[string]*settings{
		"too short":        ptr(playlistSettings(1)),
		"duplicate":        ptr(playlistSettings(1, 1)),
		"first is not map": ptr(playlistSettings(4, 1)),
		"map without mode": ptr(playlistSettings(1, 3)),
		"restricted map":   ptr(playlistSettings(1, 2)),
	} {
		if err := s.validateRoomSettings(st, 1); err == nil {
			t.Fatalf("%s: expected validation error", name)
		}
	}
}

func TestValidateRoomRandomMapAndDifficulty(t *testing.T) {
	s := validateServer()
	scenario := func(difficulty int) *settings {
		st := validRoomSettings()
		st.setNumber(6, 15, 1)
		st.setNumber(7, 3, 2)
		st.fields[45] = make([]byte, 9)
		binary.LittleEndian.PutUint16(st.fields[45], uint16(difficulty))
		st.flags[1] |= 1 << 13
		st.setNumber(40, 1, 1)
		st.flags[1] |= 1 << 8
		return &st
	}
	if err := s.validateRoomSettings(scenario(2), 1); err == nil {
		t.Fatal("random map should be rejected for Zombie Scenario")
	}
	noRandom := scenario(2)
	noRandom.flags[1] &^= 1 << 8
	if err := s.validateRoomSettings(noRandom, 1); err != nil {
		t.Fatalf("valid scenario room: %v", err)
	}
	if err := s.validateRoomSettings(scenario(3), 1); err == nil {
		t.Fatal("difficulty above map limit should be rejected")
	}
	missing := scenario(1)
	missing.flags[1] &^= 1 << 13
	if err := s.validateRoomSettings(missing, 1); err == nil {
		t.Fatal("Zombie Scenario without difficulty should be rejected")
	}
}

func TestValidateRoomFamilyBattleMode(t *testing.T) {
	s := validateServer()
	st := validRoomSettings()
	st.setNumber(6, 15, 1)
	st.setNumber(7, 3, 2)
	st.fields[45] = make([]byte, 9)
	binary.LittleEndian.PutUint16(st.fields[45], 1)
	st.flags[1] |= 1 << 13
	st.setNumber(70, 1, 1)
	if err := s.validateRoomSettings(&st, 1); err == nil {
		t.Fatal("family battle in a non-family mode should be rejected")
	}
}

func ptr(st settings) *settings { return &st }
