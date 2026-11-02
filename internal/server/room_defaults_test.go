package server

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

// TestRoomSettingsScenarioCreatePayload replays the bytes the client sends when
// creating a Zombie Scenario room: the difficulty is one byte, not the nine the
// reference reads.
func TestRoomSettingsScenarioCreatePayload(t *testing.T) {
	raw, err := hex.DecodeString("c90700000021000000000000000000004c6574277320646f20746869732100000feb000a0000000002")
	if err != nil {
		t.Fatal(err)
	}
	r := &protocol.Reader{Data: raw}
	st, err := parseSettings(r)
	if err != nil {
		t.Fatal(err)
	}
	if st.truncated || r.Pos != len(raw) {
		t.Fatalf("truncated=%v at=%d pos=%d", st.truncated, st.truncatedAt, r.Pos)
	}
	if st.number(6) != 15 || st.number(7) != 235 || st.number(8) != 10 || st.zsDifficulty() != 2 {
		t.Fatalf("mode=%d map=%d players=%d difficulty=%d", st.number(6), st.number(7), st.number(8), st.zsDifficulty())
	}
	// The client sends one byte but parses nine, so the echo pads the block.
	if len(st.fields[45]) != 9 {
		t.Fatalf("difficulty field is %d bytes", len(st.fields[45]))
	}
	w := &protocol.Writer{}
	st.write(w)
	want := append(append([]byte{}, raw...), make([]byte, 8)...)
	if !bytes.Equal(w.Data, want) {
		t.Fatalf("round trip mismatch: got %x want %x", w.Data, want)
	}
}

// TestRoomSettingsDifficultyFollowedByField makes sure the padded difficulty
// does not swallow the fields that follow it on the wire.
func TestRoomSettingsDifficultyFollowedByField(t *testing.T) {
	w := &protocol.Writer{}
	w.U32(0)
	w.U32(1<<13 | 1<<14)
	w.U32(0)
	w.U32(0)
	w.U8(2) // difficulty
	w.U8(1) // league rule
	r := &protocol.Reader{Data: w.Data}
	st, err := parseSettings(r)
	if err != nil {
		t.Fatal(err)
	}
	if st.truncated || r.Pos != len(w.Data) {
		t.Fatalf("truncated=%v pos=%d", st.truncated, r.Pos)
	}
	if len(st.fields[45]) != 9 || st.zsDifficulty() != 2 || st.number(46) != 1 {
		t.Fatalf("difficulty=%v league=%d", st.fields[45], st.number(46))
	}
}

// TestRoomSettingsFlagWithoutPayload guards the write side: a flag set without
// bytes would shift every following field on the client.
func TestRoomSettingsFlagWithoutPayload(t *testing.T) {
	var st settings
	st.flags[0] = 1 << 31
	w := &protocol.Writer{}
	st.write(w)
	if binary.LittleEndian.Uint32(w.Data[:4]) != 0 || len(w.Data) != 16 {
		t.Fatalf("dangling flag kept: %x", w.Data)
	}
}

func TestRoomSettingsTruncatedPayload(t *testing.T) {
	// lowFlag bit 31 announces 35 bytes that the client never sent. The
	// reference reader yields zeroes past the buffer, so the settings must be
	// kept instead of failing the whole request.
	w := &protocol.Writer{}
	w.U32(1 << 31)
	w.U32(0)
	w.U32(0)
	w.U32(0)
	w.Bytes([]byte{1, 2, 3})
	r := &protocol.Reader{Data: w.Data}
	st, err := parseSettings(r)
	if err != nil {
		t.Fatal(err)
	}
	if !st.truncated || st.truncatedAt != 31 || r.Pos != len(w.Data) || r.Err != nil {
		t.Fatalf("truncation not recorded: truncated=%v at=%d pos=%d err=%v", st.truncated, st.truncatedAt, r.Pos, r.Err)
	}
}

func TestRoomDefaultsFromClientTable(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	modes, err := loadModes(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{modes: modes}
	var st settings
	st.setNumber(6, 4, 1)
	st.setNumber(7, 277, 2)
	st.setNumber(8, 16, 1)
	st.setNumber(10, 20, 2)
	if !s.roomDefaults(&st, true) {
		t.Fatal("bot deathmatch missing")
	}
	if st.number(8) != 16 || st.number(10) != 20 || st.number(11) != 150 || st.number(16) != 60 || st.number(48) != 277 {
		t.Fatalf("incorrect creation defaults: capacity=%d kills=%d time=%d buy=%d map=%d", st.number(8), st.number(10), st.number(11), st.number(16), st.number(48))
	}
	if !bytes.Equal(st.fields[34], []byte{0, 4, 5, 1, 1}) {
		t.Fatalf("bots = %v", st.fields[34])
	}
	w := &protocol.Writer{}
	st.write(w)
	r := &protocol.Reader{Data: w.Data}
	parsed, err := parseSettings(r)
	if err != nil || r.Pos != len(w.Data) || !bytes.Equal(parsed.fields[34], st.fields[34]) || parsed.number(36) != 0 {
		t.Fatalf("default fields did not round trip: %v", err)
	}

	var update settings
	update.setNumber(6, 0, 1)
	if !s.roomDefaults(&update, false) {
		t.Fatal("original mode missing")
	}
	st.merge(update)
	if st.number(7) != 277 || st.number(8) != 32 || st.number(10) != 0 || st.number(12) != 2 || st.number(36) != 800 || !bytes.Equal(st.fields[34], make([]byte, 5)) {
		t.Fatal("changing mode failed to reset gameplay defaults or preserve map")
	}
	st.setNumber(6, 255, 1)
	if s.roomDefaults(&st, true) {
		t.Fatal("accepted unknown mode")
	}
}
