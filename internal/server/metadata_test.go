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

	tooBig := bytes.Repeat([]byte("y"), metadataChunkSize*5)
	if chunks = zipMetadataFrames(33, tooBig, true); len(chunks) != metadataMaxChunks {
		t.Fatalf("truncated chunks %d", len(chunks))
	}
	if frames = zipMetadataFrames(33, tooBig, false); frames != nil {
		t.Fatalf("legacy framing accepted an oversized payload: %d frames", len(frames))
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
	// The client reads a length after the type byte for ItemBox; without it the
	// leading zero and the low count byte are taken as the length.
	if data[0] != 4 {
		t.Fatalf("item box type %v", data[0])
	}
	size := int(data[1]) | int(data[2])<<8
	if size != len(data)-3 {
		t.Fatalf("item box size %d, body %d", size, len(data)-3)
	}
	if data[3] != 0 {
		t.Fatalf("item box unknown byte %v", data[3])
	}
	count := uint32(data[4]) | uint32(data[5])<<8 | uint32(data[6])<<16 | uint32(data[7])<<24
	if count == 0 {
		t.Fatal("item box count is zero")
	}
	// Each entry is 26 bytes after the 8-byte header.
	if len(data) != 8+int(count)*26 {
		t.Fatalf("item box size %d for %d entries", len(data), count)
	}
}
