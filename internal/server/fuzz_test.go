package server

import (
	"encoding/binary"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// FuzzItemRequest feeds arbitrary payloads to the item dispatcher. It is a crash
// fuzzer, not an equivalence test: every kind reads a different field layout off
// the wire, so a missed bounds check would panic before any handler could reject
// the request.
func FuzzItemRequest(f *testing.F) {
	dir, err := os.MkdirTemp("", "fuzz-item-request")
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { os.RemoveAll(dir) })
	db, err := store.Open(filepath.Join(dir, "fuzz.db3"))
	if err != nil {
		f.Fatal(err)
	}
	f.Cleanup(func() { db.Close() })
	if err = db.Register("fuzzuser", "secret1", "127.0.0.1", 0); err != nil {
		f.Fatal(err)
	}
	if err = db.CreateCharacter(1, "FuzzUser", config.DefaultUser{Level: 1}); err != nil {
		f.Fatal(err)
	}
	s := &Server{
		Store:  db,
		Config: config.Config{InventorySlotMax: 10},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		items: map[int]itemDefinition{
			12: {
				Category: 2,
			},
			204: {
				Category: 2,
			},
			8481: {
				Category: 2,
			},
		},
		weaponPaints: map[int][]int{
			12: {500, 501},
		},
		enhanceMax: map[int]enhanceLimit{
			12: {
				TotalMaxLv: 4,
				Caps:       [7]int{3, 3, 3, 3, 3, 3, 3},
			},
		},
		enhanceExp: map[int][]int{
			0: {1, 1, 1, 1, 1, 1, 1},
		},
	}
	c := &client{
		user: store.User{
			ID: 1,
		},
		character: store.Character{
			ID:   1,
			Name: "FuzzUser",
		},
		queue: make(chan outbound, 64),
	}
	// Seeds cover the kinds with the most varied field layouts.
	f.Add([]byte{0, 0, 1, 0, 0, 1, 0})
	f.Add([]byte{4, 1, 0, 2, 0})
	f.Add([]byte{7, 1, 0, 0, 0})
	f.Add([]byte{14, 0, 0, 1, 0, 1, 0, 1, 0})
	f.Add([]byte{16, 0, 1, 0, 5, 0})
	f.Add([]byte{17, 1, 0, 2, 0})
	f.Add([]byte{23, 0, 1, 0})
	f.Add([]byte{26, 0, 1, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > protocol.MaxPayload {
			return
		}
		// Every iteration starts with an empty outbox so a full queue cannot make
		// the target skip work that would otherwise run.
		for len(c.queue) > 0 {
			<-c.queue
		}
		_ = s.itemRequest(c, &protocol.Reader{Data: data})
	})
}

// FuzzMetadataFrames fuzzes the metadata framing. Every emitted frame must fit
// the client's budget and describe itself consistently: the length field has to
// match the bytes that follow, otherwise the client reads into the next frame.
func FuzzMetadataFrames(f *testing.F) {
	f.Add(0, []byte{}, false)
	f.Add(4, []byte{1, 2, 3}, false)
	f.Add(16, make([]byte, 64000), true)
	f.Add(16, make([]byte, 64001), true)
	f.Fuzz(func(t *testing.T, id int, data []byte, zipType bool) {
		if len(data) > metadataChunkSize*metadataMaxChunks+1 {
			return
		}
		frames := zipMetadataFrames(id, data, zipType)
		total := 0
		for _, frame := range frames {
			if len(frame) > protocol.MaxPayload {
				t.Fatalf("frame carries %d bytes, payload limit is %d", len(frame), protocol.MaxPayload)
			}
			if frame[0] != byte(id) {
				t.Fatalf("frame id %d for metadata id %d", frame[0], id)
			}
			header := 3
			if zipType {
				header = 4
			}
			size := int(binary.LittleEndian.Uint16(frame[header-2 : header]))
			if size != len(frame)-header {
				t.Fatalf("length field %d does not match %d payload bytes", size, len(frame)-header)
			}
			total += size
		}
		if len(frames) != 0 && total != len(data) {
			t.Fatalf("%d frames carry %d of %d bytes", len(frames), total, len(data))
		}
	})
}
