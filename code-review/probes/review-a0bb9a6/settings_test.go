package server

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

// Widths transcribed from baseline CRoomSettings/WriteSettings. Variable arrays
// and strings use their empty encoding; difficulty uses the documented client
// exception (one byte in, nine bytes out).
func TestReviewA0OuterSettingsWidthMatrix(t *testing.T) {
	var widths [128]int
	for i := 0; i < 64; i++ {
		widths[i] = 1
	}
	for i := 64; i <= 72; i++ {
		widths[i] = 1
	}
	widths[2] = 7
	widths[7] = 2
	widths[10] = 2
	widths[31] = 35
	widths[32] = 8
	widths[34] = 5
	widths[36] = 2
	widths[48] = 2
	widths[49] = 17
	widths[50] = 4
	widths[51] = 0
	widths[63] = 0
	widths[65] = 5
	widths[73] = 10
	widths[127] = 1
	for index, width := range widths {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			w := &protocol.Writer{}
			for group := range 4 {
				flag := 0
				if index/32 == group {
					flag = 1 << (index % 32)
				}
				w.U32(flag)
			}
			w.Bytes(make([]byte, width))
			r := &protocol.Reader{Data: w.Data}
			st, err := parseSettings(r)
			if err != nil || st.truncated || r.Pos != len(w.Data) {
				t.Fatalf("field %d width=%d rejected: %v truncated=%v", index, width, err, st.truncated)
			}
			out := &protocol.Writer{}
			st.write(out)
			expected := append([]byte(nil), w.Data...)
			if index == 45 {
				expected = append(expected, make([]byte, 8)...)
			}
			if !bytes.Equal(out.Data, expected) {
				t.Fatalf("field %d changed encoding: got=%d want=%d", index, len(out.Data), len(expected))
			}
		})
	}
}
