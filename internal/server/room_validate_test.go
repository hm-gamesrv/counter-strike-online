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
			3: {
				MaxPlayers:       16,
				CreateMinPlayers: 1,
			},
			9: {
				MaxPlayers:       16,
				CreateMinPlayers: 1,
			},
			15: {
				MaxPlayers:       8,
				CreateMinPlayers: 1,
			},
			16: {
				MaxPlayers:       16,
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
			5: {
				MaxPlayer: 16,
				Values:    map[string]int{"map_id": 5, "map_zombi_expand": 1},
			},
			6: {
				MaxPlayer: 16,
				Values:    map[string]int{"map_id": 6, "map_original": 1, "weapon_restrict_deault": 5},
			},
			7: {
				MaxPlayer: 16,
				Values:    map[string]int{"map_id": 7, "map_bot_original": 1, "ball_max": 2},
			},
			8: {
				MaxPlayer: 16,
				Values:    map[string]int{"map_id": 8, "map_FunMode": 1, "Fun_Subtype(1)": 16, "ball_max": 2},
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

func TestSanitizeOptionalRoomFields(t *testing.T) {
	s := validateServer()
	s.items = map[int]itemDefinition{1: {}, 2: {}}

	st := validRoomSettings()
	st.fields[50] = []byte{1, 0, 0, 0, 0}
	st.flags[1] |= 1 << 18
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(50) {
		t.Fatal("voxel block kept on a non-studio map")
	}

	st = validRoomSettings()
	st.setNumber(64, 0, 1)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(64) {
		t.Fatal("fireBomb kept for mode 0")
	}

	st = validRoomSettings()
	st.setNumber(66, 40, 1)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(66) {
		t.Fatal("mutation limit kept outside mode 45")
	}
	st = validRoomSettings()
	st.setNumber(66, 20, 1)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if !st.hasField(66) {
		t.Fatal("default mutation limit was cleared")
	}

	zbLimit := func(values [4]int) []byte {
		b := []byte{0}
		for _, value := range values {
			b = binary.LittleEndian.AppendUint32(b, uint32(value))
		}
		return b
	}
	st = validRoomSettings()
	st.setNumber(6, 9, 1)
	st.setNumber(7, 5, 2)
	st.fields[49] = zbLimit([4]int{1, 1, 0, 0})
	st.flags[1] |= 1 << 17
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(49) {
		t.Fatal("duplicate zbLimit kept")
	}
	st = validRoomSettings()
	st.setNumber(6, 9, 1)
	st.setNumber(7, 5, 2)
	st.fields[49] = zbLimit([4]int{1, 2, 0, 0})
	st.flags[1] |= 1 << 17
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if !st.hasField(49) {
		t.Fatal("valid zbLimit was dropped")
	}

	st = validRoomSettings()
	st.setNumber(6, 15, 1)
	st.setNumber(7, 3, 2)
	st.setNumber(54, 1, 1)
	st.fields[45] = make([]byte, 9)
	binary.LittleEndian.PutUint16(st.fields[45], 1)
	st.flags[1] |= 1 << 13
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(54) {
		t.Fatal("teamSwitch kept for mode 15")
	}

	st = validRoomSettings()
	st.setNumber(16, 33, 1)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(16) {
		t.Fatal("invalid buy time kept")
	}
	st = validRoomSettings()
	st.setNumber(36, 123, 1)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(36) {
		t.Fatal("invalid starting cash kept")
	}
	st = validRoomSettings()
	st.setNumber(6, 15, 1)
	st.setNumber(7, 3, 2)
	st.fields[45] = make([]byte, 9)
	binary.LittleEndian.PutUint16(st.fields[45], 1)
	st.flags[1] |= 1 << 13
	st.setNumber(18, 0, 1)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(18) {
		t.Fatal("team balance changed for a mode that disallows it")
	}
	st = validRoomSettings()
	st.setNumber(28, 0x1f, 1)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if len(st.fields[28]) != 1 || st.fields[28][0] != 0x01 {
		t.Fatalf("view flags not cleaned: %v", st.fields[28])
	}

	st = validRoomSettings()
	st.fields[34] = []byte{5, 4, 5, 0, 1}
	st.flags[1] |= 1 << 2
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(34) {
		t.Fatal("bot settings kept for a mode without bots")
	}
	st = validRoomSettings()
	st.setNumber(6, 3, 1)
	st.setNumber(7, 7, 2)
	st.fields[34] = []byte{9, 20, 0, 0, 1}
	st.flags[1] |= 1 << 2
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if got := st.fields[34]; len(got) != 5 || got[0] != 7 || got[1] != 15 || got[2] != 1 {
		t.Fatalf("bot clamp %v", got)
	}
	st = validRoomSettings()
	st.setNumber(6, 3, 1)
	st.setNumber(7, 7, 2)
	st.setNumber(38, 3, 1)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(38) {
		t.Fatal("ball number kept for a mode without football")
	}
	st = validRoomSettings()
	st.setNumber(6, 16, 1)
	st.setNumber(7, 8, 2)
	st.setNumber(38, 1, 1)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if !st.hasField(38) {
		t.Fatal("valid ball number was dropped")
	}
	st = validRoomSettings()
	st.setNumber(6, 16, 1)
	st.setNumber(7, 8, 2)
	st.setNumber(38, 3, 1)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.hasField(38) {
		t.Fatal("ball number above the map limit was kept")
	}
	st = validRoomSettings()
	st.setNumber(69, 20, 1)
	st.flags[2] |= 1 << 5
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.number(69) != 13 {
		t.Fatalf("weapon restriction clamp %d", st.number(69))
	}
	st = validRoomSettings()
	st.setNumber(7, 6, 2)
	if err := s.validateRoomSettings(&st, 1); err != nil {
		t.Fatal(err)
	}
	if st.number(69) != 5 {
		t.Fatalf("map default weapon restriction %d", st.number(69))
	}
}
