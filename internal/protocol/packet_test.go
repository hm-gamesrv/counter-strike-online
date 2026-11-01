package protocol

import (
	"bytes"
	"crypto/md5"
	"crypto/rc4"
	"io"
	"testing"
)

type fragmented struct{ io.Reader }

func (f fragmented) Read(b []byte) (int, error) {
	if len(b) > 1 {
		b = b[:1]
	}
	return f.Reader.Read(b)
}
func TestFramesFragmentationEncryptionAndWrap(t *testing.T) {
	for _, encrypted := range []bool{false, true} {
		a, b := NewCodec(), NewCodec()
		if encrypted {
			key, iv := DeriveKey([]byte("0123456789abcdef"))
			want := md5.Sum([]byte("0123456789abcdef"))
			if !bytes.Equal(key[:16], want[:]) || bytes.Equal(iv[:16], want[:]) {
				t.Fatal("key derivation")
			}
			a.Out, _ = rc4.NewCipher(key[:16])
			b.In, _ = rc4.NewCipher(key[:16])
		}
		var stream bytes.Buffer
		for i := 0; i < 600; i++ {
			if e := a.Write(&stream, 67, []byte("hello\x00")); e != nil {
				t.Fatal(e)
			}
		}
		r := fragmented{&stream}
		for i := 0; i < 600; i++ {
			p, e := b.Read(r)
			if e != nil {
				t.Fatal(i, e)
			}
			if p.Sequence != byte(i+1) || p.ID != 67 || string(p.Data) != "hello\x00" {
				t.Fatal(p)
			}
		}
	}
}
func TestRejectMalformedFrames(t *testing.T) {
	for _, b := range [][]byte{{'X', 1, 1, 0, 0}, {'U', 2, 1, 0, 0}, {'U', 1, 0, 0}, {'U', 1, 255, 255}, {'U', 1, 3, 0, 0}} {
		if _, e := NewCodec().Read(bytes.NewReader(b)); e == nil {
			t.Fatalf("accepted %x", b)
		}
	}
	if e := NewCodec().Write(io.Discard, 1, make([]byte, MaxFrame)); e == nil {
		t.Fatal("accepted oversized output")
	}
}
func TestReaderRejectsMissingTerminatorAndTruncation(t *testing.T) {
	r := &Reader{Data: []byte("no terminator")}
	r.CString()
	if r.Err == nil {
		t.Fatal("unterminated string accepted")
	}
	r = &Reader{Data: []byte{1}}
	r.U32()
	if r.Err == nil {
		t.Fatal("short integer accepted")
	}
}
func FuzzFrame(f *testing.F) {
	f.Add([]byte{'U', 1, 1, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		p, e := NewCodec().Read(bytes.NewReader(b))
		if e == nil {
			if len(p.Data)+5 > MaxFrame {
				t.Fatal("oversize")
			}
			r := &Reader{Data: p.Data}
			r.U32()
			r.CString()
			r.Bytes(65536)
		}
	})
}
