package server

// Probe: code-review/probes/review-75cddae-store/server_test.go
//
// The settings delta path (roomRequest kind 5) parses an attacker-controlled
// flag table, merges it into the live room, and re-serializes the result. It is
// the one place in the lobby where the server derives a wire frame from state it
// only half understood, so a divergence between flags and fields turns into a
// frame the client cannot parse.
//
// Run with:
//   go test ./internal/server -overlay code-review/probes/review-75cddae-store/overlay.json \
//     -run '^$' -fuzz '^FuzzRoomUpdateSettings$' -fuzztime 180s

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// cloneSettings deep-copies the field slices so a fuzz iteration cannot mutate
// the seed the next iteration starts from.
func cloneSettings(s settings) settings {
	out := s
	for i, field := range s.fields {
		if field != nil {
			out.fields[i] = append([]byte{}, field...)
		}
	}
	return out
}

// settingsPayload serializes a delta without the leading kind byte.
func settingsPayload(st settings) []byte {
	w := &protocol.Writer{}
	st.write(w)
	return w.Data
}

func FuzzRoomUpdateSettings(f *testing.F) {
	dir, err := os.MkdirTemp("", "fuzz-room-settings")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { os.RemoveAll(dir) })
	db, err := store.Open(filepath.Join(dir, "fuzz.db3"))
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { db.Close() })
	if err = db.Register("fuzzroom", "secret1", "127.0.0.1", 0); err != nil {
		f.Fatal(err)
	}
	if err = db.CreateCharacter(1, "FuzzRoom", config.DefaultUser{Level: 1}); err != nil {
		f.Fatal(err)
	}
	s := &Server{
		Store: db,
		Config: config.Config{
			InventorySlotMax: 10,
		},
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		items: map[int]itemDefinition{},
		modes: map[int]modeDefinition{},
		rooms: map[int]*room{},
		users: map[int]*client{},
	}
	c := &client{
		user: store.User{
			ID: 1,
		},
		character: store.Character{
			ID:   1,
			Name: "FuzzRoom",
		},
		// Large enough that the queue never reaches send()'s conn.Close() branch,
		// which would nil-deref in a harness that has no socket behind the client.
		queue: make(chan outbound, 8192),
	}
	base := validRoomSettings()
	r := &room{
		id:       1,
		host:     c,
		members:  []*client{c},
		settings: cloneSettings(base),
	}
	s.rooms[1] = r
	s.users[1] = c
	c.room = 1

	drain := func() {
		for len(c.queue) > 0 {
			<-c.queue
		}
	}
	single := func(index, value, width int) settings {
		var st settings
		st.setNumber(index, value, width)
		return st
	}

	f.Add([]byte{})
	f.Add(settingsPayload(base))
	f.Add(settingsPayload(single(8, 12, 1)))
	f.Add(settingsPayload(single(6, 0, 1)))
	f.Add(settingsPayload(single(7, 1, 2)))
	f.Add(settingsPayload(single(70, 1, 1)))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})

	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > protocol.MaxPayload-1 {
			return
		}
		drain()
		// Every iteration starts from the same room so a failing input replays
		// on its own: the corpus entry is the whole input, not a delta of deltas.
		r.settings = cloneSettings(base)
		frame := make([]byte, 0, len(payload)+1)
		frame = append(frame, 5)
		frame = append(frame, payload...)
		_ = s.roomRequest(c, &protocol.Reader{Data: frame})
		drain()

		// Invariant 1: the stored settings must serialize to a frame that this
		// same parser consumes whole and reconstructs identically. A flag bit
		// without a field (or the reverse) shifts every following field, so the
		// client would read the next block out of the wrong bytes.
		w := &protocol.Writer{}
		r.settings.write(w)
		if len(w.Data) > protocol.MaxPayload {
			t.Fatalf("stored settings serialize to %d bytes, payload limit is %d", len(w.Data), protocol.MaxPayload)
		}
		//
		// Field 45 (group 1 bit 13, difficulty) is the one documented asymmetry:
		// parseSettings consumes a single byte but pads the stored value to nine,
		// because the client writes one byte and reads nine back. The comparison
		// target is therefore the stored set with that pad removed.
		want := r.settings
		pad := 0
		if len(want.fields[45]) == 9 {
			// The server reader takes 1 byte and pads back to 9, so replaying the
			// server's own frame leaves the 8 pad bytes to be re-read as the
			// following fields: the widths survive, the values shift.
			pad = 8
		}
		reader := &protocol.Reader{Data: w.Data}
		back, err := parseSettings(reader)
		if err != nil {
			t.Fatalf("stored settings do not re-parse: %v (layout %q)", err, r.settings.layout())
		}
		if back.truncated {
			t.Fatalf("stored settings re-parse truncated at field %d (layout %q)", back.truncatedAt, r.settings.layout())
		}
		if reader.Pos != len(w.Data)-pad {
			t.Fatalf("stored settings re-parse consumed %d of %d bytes (layout %q)", reader.Pos, len(w.Data), r.settings.layout())
		}
		if back.flags != want.flags {
			t.Fatalf("re-parsed flags %v, stored flags %v", back.flags, want.flags)
		}
		for i := range back.fields {
			if len(back.fields[i]) != len(want.fields[i]) {
				t.Fatalf("field %d re-parsed with %d bytes, stored with %d (layout %q)", i, len(back.fields[i]), len(want.fields[i]), r.settings.layout())
			}
		}

		// Invariant 2: the accepted update never shrinks room capacity below the
		// players already seated in it.
		if capacity := r.settings.number(8); capacity >= 1 && len(r.members) > capacity {
			t.Fatalf("settings accept %d members with capacity %d", len(r.members), capacity)
		}
	})
}

// TestRoomSettingsZeroWidthFieldEcho pins the input FuzzRoomUpdateSettings found
// while this probe was being written (preserved at
// internal/server/testdata/fuzz/FuzzRoomUpdateSettings/7742cbf9344e819e).
//
// parseSettings consumes zero bytes for group 2 bits 10..31 and group 3 bits
// 0..30, but it still records a flag bit plus an empty (non-nil) field for each.
// settings.write then emits the flag with no payload. The reference rebuilds the
// outgoing frame from the fields it models (roomsettings.cpp CheckNewSettings
// clears every ROOM_*_UNK bit it read), so it never re-announces an unmodeled
// bit; this build keeps and rebroadcasts it.
func TestRoomSettingsZeroWidthFieldEcho(t *testing.T) {
	r := &protocol.Reader{Data: []byte("1\x00\x00\x0000\x00\x00\x0000000000\x000000000")}
	st, err := parseSettings(r)
	if err != nil {
		t.Fatal(err)
	}
	if st.truncated {
		t.Fatalf("payload truncated at %d", st.truncatedAt)
	}
	var zero []int
	for group, bits := range st.flags {
		for bit := 0; bit < 32; bit++ {
			if bits&(1<<bit) == 0 {
				continue
			}
			if index := group*32 + bit; len(st.fields[index]) == 0 {
				zero = append(zero, index)
			}
		}
	}
	if len(zero) == 0 {
		t.Skip("this build no longer records zero-width flagged fields")
	}
	t.Fatalf("%d flagged settings fields carry no bytes (%v); they are stored and rebroadcast as zero-length fields, so the room re-announces flag bits the reference strips", len(zero), zero)
}
