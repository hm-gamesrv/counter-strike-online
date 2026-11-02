package server

import (
	"archive/zip"
	"bytes"
	"io"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func TestMetadataZipFrames(t *testing.T) {
	frames := zipMetadataFrames(24, []byte("small"), false)
	if len(frames) != 1 {
		t.Fatalf("frames %d", len(frames))
	}
	r := &protocol.Reader{Data: frames[0]}
	if r.U8() != 24 || r.U16() != 5 || string(r.Bytes(5)) != "small" {
		t.Fatalf("small frame %v", frames[0])
	}
	if r.Pos != len(frames[0]) {
		t.Fatal("trailing bytes in legacy frame")
	}

	typed := zipMetadataFrames(24, []byte("small"), true)
	if len(typed) != 1 {
		t.Fatalf("typed frames %d", len(typed))
	}
	r = &protocol.Reader{Data: typed[0]}
	if r.U8() != 24 || r.U8() != metadataFullZip || r.U16() != 5 || string(r.Bytes(5)) != "small" {
		t.Fatalf("typed frame %v", typed[0])
	}

	payload := bytes.Repeat([]byte("x"), metadataChunkSize*2+10)
	chunks := zipMetadataFrames(32, payload, true)
	if len(chunks) != 3 {
		t.Fatalf("chunk count %d", len(chunks))
	}
	rest := 0
	for i, frame := range chunks {
		r := &protocol.Reader{Data: frame}
		if r.U8() != 32 {
			t.Fatalf("chunk id %d", i)
		}
		marker := r.U8()
		want := i + 1
		if i == len(chunks)-1 {
			want = metadataMaxChunks
		}
		if marker != want {
			t.Fatalf("chunk %d marker %d, want %d", i, marker, want)
		}
		size := int(r.U16())
		if size > metadataChunkSize || r.Pos+size != len(frame) {
			t.Fatalf("chunk %d size %d", i, size)
		}
		rest += size
	}
	if rest != len(payload) {
		t.Fatalf("chunk payload %d, want %d", rest, len(payload))
	}

	// A payload beyond four chunks must not be sent as a truncated archive.
	tooBig := bytes.Repeat([]byte("y"), metadataChunkSize*metadataMaxChunks+1)
	if chunks = zipMetadataFrames(33, tooBig, true); chunks != nil {
		t.Fatalf("chunked framing truncated an oversized payload into %d frames", len(chunks))
	}
	if frames = zipMetadataFrames(33, tooBig, false); frames != nil {
		t.Fatalf("legacy framing accepted an oversized payload: %d frames", len(frames))
	}

	// The metadata header costs three bytes, so the single-frame zip budget is
	// MaxPayload-3 (65528 for the current transport).
	exact := bytes.Repeat([]byte("z"), protocol.MaxPayload-3)
	if frames = zipMetadataFrames(34, exact, false); len(frames) != 1 {
		t.Fatalf("boundary payload rejected: %d frames", len(frames))
	}
	over := bytes.Repeat([]byte("z"), protocol.MaxPayload-2)
	if frames = zipMetadataFrames(34, over, false); frames != nil {
		t.Fatalf("payload over the frame budget accepted: %d bytes", len(frames[0]))
	}
}

func TestMetadataDispatchOrder(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	enabled := map[string]bool{}
	for _, entry := range metadataFiles {
		enabled[entry.Name] = true
	}
	for name := range metadataTypeIDs {
		enabled[name] = true
	}
	packets, err := loadMetadata(dir, config.Config{Metadata: enabled})
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) < 20 {
		t.Fatalf("packets %d", len(packets))
	}
	order := map[int]int{}
	for i, name := range metadataOrder {
		if id, ok := metadataTypeIDs[name]; ok {
			order[id] = i
			continue
		}
		for _, entry := range metadataFiles {
			if entry.Name == name {
				order[entry.ID] = i
				break
			}
		}
	}
	previous := -1
	seen := map[int]bool{}
	for _, packet := range packets {
		id := int(packet[0])
		position, ok := order[id]
		if !ok {
			t.Fatalf("metadata type %d is not in metadataOrder", id)
		}
		if position <= previous {
			t.Fatalf("metadata type %d arrives out of the baseline order", id)
		}
		previous = position
		seen[id] = true
	}
	// Spot-check the relations the previous implementation got wrong: the built
	// packets sat at the end of the stream instead of their baseline slots.
	if !seen[4] || !seen[6] || !seen[15] || !seen[16] {
		t.Fatalf("built metadata types missing: %v", seen)
	}
	if order[4] > order[8] || order[6] > order[8] || order[16] < order[42] {
		t.Fatal("built metadata types are not in their baseline slots")
	}
}

func TestLoadMetadataZipPayload(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Metadata: map[string]bool{"Maplist": true, "GameModeList": true}}
	packets, err := loadMetadata(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 2 {
		t.Fatalf("packets %d", len(packets))
	}
	ids := map[int]bool{}
	for _, packet := range packets {
		r := &protocol.Reader{Data: packet}
		id := r.U8()
		ids[id] = true
		size := int(r.U16())
		if size != len(packet)-3 {
			t.Fatalf("metadata %d size %d, payload %d", id, size, len(packet)-3)
		}
		raw := r.Bytes(size)
		if len(raw) < 30 || string(raw[:4]) != "PK\x03\x04" {
			t.Fatalf("metadata %d missing local header", id)
		}
		flags := uint16(raw[6]) | uint16(raw[7])<<8
		compressedSize := uint64(raw[18]) | uint64(raw[19])<<8 | uint64(raw[20])<<16 | uint64(raw[21])<<24
		uncompressedSize := uint64(raw[22]) | uint64(raw[23])<<8 | uint64(raw[24])<<16 | uint64(raw[25])<<24
		if flags&0x8 != 0 || compressedSize == 0 || uncompressedSize == 0 {
			t.Fatalf("metadata %d uses a data descriptor (flags=%x sizes=%d/%d)", id, flags, compressedSize, uncompressedSize)
		}
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(size))
		if err != nil {
			t.Fatalf("metadata %d zip: %v", id, err)
		}
		want := map[int]string{0: "MapList.csv", 24: "GameModeList.csv"}[id]
		if len(zr.File) != 1 || zr.File[0].Name != want {
			t.Fatalf("metadata %d entries %v", id, zr.File)
		}
		rc, err := zr.File[0].Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil || len(content) < 100 {
			t.Fatalf("metadata %d content %d %v", id, len(content), err)
		}
	}
	if !ids[0] || !ids[24] {
		t.Fatalf("metadata ids %v", ids)
	}
}

// TestLoadMetadataZipTypeByte pins the opt-in upstream framing: the client this
// server targets reads the length directly after the id, so the zip-type byte
// must stay off unless the configuration enables it.
func TestLoadMetadataZipTypeByte(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Metadata:            map[string]bool{"Maplist": true},
		MetadataZipTypeByte: true,
	}
	packets, err := loadMetadata(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 {
		t.Fatalf("packets %d", len(packets))
	}
	r := &protocol.Reader{Data: packets[0]}
	if r.U8() != 0 || r.U8() != metadataFullZip {
		t.Fatalf("zip type byte missing: %v", packets[0][:4])
	}
	if size := int(r.U16()); size != len(packets[0])-4 {
		t.Fatalf("size %d payload %d", size, len(packets[0])-4)
	}
}

func TestMetadataItemBoxHeader(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{Metadata: map[string]bool{"ItemBox": true}}
	packets, err := loadMetadata(dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 1 || len(packets[0]) < 6 {
		t.Fatalf("packets %d", len(packets))
	}
	data := packets[0]
	// CSNZ_Server@12644f2 writes the type and the entry count only; adding the
	// later build's "unknown" byte or a length prefix makes the client misread
	// the count and crash.
	if data[0] != 4 {
		t.Fatalf("item box type %v", data[0])
	}
	count := uint32(data[1]) | uint32(data[2])<<8 | uint32(data[3])<<16 | uint32(data[4])<<24
	if count == 0 {
		t.Fatal("item box count is zero")
	}
	if len(data) != 5+int(count)*26 {
		t.Fatalf("item box size %d for %d entries", len(data), count)
	}
}
