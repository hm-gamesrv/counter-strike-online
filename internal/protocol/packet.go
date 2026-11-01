// Package protocol implements CSNZ's little-endian U frames.
package protocol

import (
	"bytes"
	"crypto/md5"
	"crypto/rc4"
	"encoding/binary"
	"errors"
	"io"
)

const MaxFrame = 65536
const MaxPayload = MaxFrame - 5
const Greeting = "~SERVERCONNECTED\n"

type Packet struct {
	Sequence, ID byte
	Data         []byte
}

// Codec owns continuous, independent input/output RC4 streams. One goroutine
// must own each direction. Sequence numbers wrap from 255 to 0.
type Codec struct {
	In, Out                       *rc4.Cipher
	ReceiveSequence, SendSequence byte
}

func NewCodec() *Codec { return &Codec{ReceiveSequence: 1, SendSequence: 1} }
func (c *Codec) Read(r io.Reader) (Packet, error) {
	var p Packet
	h := make([]byte, 4)
	if _, err := io.ReadFull(r, h); err != nil {
		return p, err
	}
	if c.In != nil {
		c.In.XORKeyStream(h, h)
	}
	if h[0] != 'U' {
		return p, errors.New("invalid frame signature")
	}
	if h[1] != c.ReceiveSequence {
		return p, errors.New("invalid frame sequence")
	}
	n := int(binary.LittleEndian.Uint16(h[2:]))
	if n < 1 || n+4 > MaxFrame {
		return p, errors.New("invalid frame length")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return p, err
	}
	if c.In != nil {
		c.In.XORKeyStream(b, b)
	}
	c.ReceiveSequence++
	return Packet{h[1], b[0], b[1:]}, nil
}
func (c *Codec) Write(w io.Writer, id byte, data []byte) error {
	if len(data) > MaxPayload {
		return errors.New("frame too large")
	}
	b := make([]byte, 5+len(data))
	b[0] = 'U'
	b[1] = c.SendSequence
	binary.LittleEndian.PutUint16(b[2:], uint16(len(data)+1))
	b[4] = id
	copy(b[5:], data)
	if c.Out != nil {
		c.Out.XORKeyStream(b, b)
	}
	for len(b) > 0 {
		n, e := w.Write(b)
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		b = b[n:]
	}
	c.SendSequence++
	return nil
}

// DeriveKey matches EVP_BytesToKey(AES-128-CBC, MD5, no salt, count=1).
// The legacy protocol uses the first digest as its RC4 key.
func DeriveKey(hwid []byte) (key, iv [64]byte) {
	a := md5.Sum(hwid)
	copy(key[:], a[:])
	b := md5.Sum(append(append([]byte{}, a[:]...), hwid...))
	copy(iv[:], b[:])
	return
}

type Reader struct {
	Data []byte
	Pos  int
	Err  error
}

func (r *Reader) Bytes(n int) []byte {
	if r.Err != nil {
		return nil
	}
	if n < 0 || n > len(r.Data)-r.Pos {
		r.Err = io.ErrUnexpectedEOF
		return nil
	}
	b := r.Data[r.Pos : r.Pos+n]
	r.Pos += n
	return b
}
func (r *Reader) U8() int {
	b := r.Bytes(1)
	if b == nil {
		return 0
	}
	return int(b[0])
}
func (r *Reader) U16() int {
	b := r.Bytes(2)
	if b == nil {
		return 0
	}
	return int(binary.LittleEndian.Uint16(b))
}
func (r *Reader) U32() int {
	b := r.Bytes(4)
	if b == nil {
		return 0
	}
	return int(binary.LittleEndian.Uint32(b))
}
func (r *Reader) I32() int { return int(int32(r.U32())) }
func (r *Reader) CString() string {
	if r.Err != nil {
		return ""
	}
	i := bytes.IndexByte(r.Data[r.Pos:], 0)
	if i < 0 {
		r.Err = io.ErrUnexpectedEOF
		return ""
	}
	b := r.Bytes(i + 1)
	return string(b[:i])
}

type Writer struct{ Data []byte }

func (w *Writer) U8(v int)        { w.Data = append(w.Data, byte(v)) }
func (w *Writer) U16(v int)       { w.Data = binary.LittleEndian.AppendUint16(w.Data, uint16(v)) }
func (w *Writer) U32(v int)       { w.Data = binary.LittleEndian.AppendUint32(w.Data, uint32(v)) }
func (w *Writer) U64(v int64)     { w.Data = binary.LittleEndian.AppendUint64(w.Data, uint64(v)) }
func (w *Writer) String(v string) { w.Data = append(w.Data, v...); w.U8(0) }
func (w *Writer) Bytes(v []byte)  { w.Data = append(w.Data, v...) }
