package server

import (
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

func TestValidateRoomSettings(t *testing.T) {
	s := &Server{
		Config: config.Config{},
		modes: map[int]modeDefinition{0: {
			MaxPlayers:       32,
			CreateMinPlayers: 2,
			WinLimit:         limitRange{Min: 5, Max: 33, Step: 4},
			KillLimit:        limitRange{Min: 20, Max: 120, Step: 20},
			TimeLimit:        limitRange{Min: 150, Max: 150},
			RoundTimeLimit:   limitRange{Min: 2, Max: 3, Step: 1},
		}},
		mapMax: map[int]int{1: 20},
	}
	s.Config.Room.ValidateSettings = true
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
