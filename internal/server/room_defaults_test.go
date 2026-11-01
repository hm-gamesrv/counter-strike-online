package server

import (
	"bytes"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

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
