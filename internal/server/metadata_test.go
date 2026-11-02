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
	frames := zipMetadataFrames(24, []byte("small"))
	if len(frames) != 1 {
		t.Fatalf("frames %d", len(frames))
	}
	r := &protocol.Reader{Data: frames[0]}
	if r.U8() != 24 || r.U8() != metadataFullZip || r.U16() != 5 || string(r.Bytes(5)) != "small" {
		t.Fatalf("small frame %v", frames[0])
	}
	if r.Pos != len(frames[0]) {
		t.Fatal("trailing bytes in full zip frame")
	}

	payload := bytes.Repeat([]byte("x"), metadataChunkSize*2+10)
	chunks := zipMetadataFrames(32, payload)
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
	if chunks = zipMetadataFrames(33, tooBig); len(chunks) != metadataMaxChunks {
		t.Fatalf("truncated chunks %d", len(chunks))
	}
}

func TestLoadMetadataSendsZipType(t *testing.T) {
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
		if r.U8() != metadataFullZip {
			t.Fatalf("metadata %d missing zip type", id)
		}
		size := int(r.U16())
		if size != len(packet)-4 {
			t.Fatalf("metadata %d size %d, payload %d", id, size, len(packet)-4)
		}
		zr, err := zip.NewReader(bytes.NewReader(r.Bytes(size)), int64(size))
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
