package server

import (
	"encoding/binary"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

// userInfoFields is the part of the reference user info the tests assert on.
type userInfoFields struct {
	LowFlags    uint32
	HighFlags   uint32
	NameplateID uint32
	GameName    string
	GameName2   string
	Level       uint32
	Exp         uint64
	Cash        uint64
	Points      uint64
	Battles     uint32
	Win         uint32
	Kills       uint32
	Deaths      uint32
	ClanID      uint32
	ClanMarkID  uint32
	ClanName    string
	Tournament  int
	PrefixID    int
	HonorPoints uint32
	Bitmap      []byte
	Titles      [5]int
	Mileage     uint32
	CharacterID uint32
	ChatColorID int
	Consumed    int
}

// parseUserInfoBody walks one user info block. Every flag bit is set (the
// reference always passes UFLAG_LOW_ALL|UFLAG_HIGH_ALL, see
// packethelper_fulluserinfo.cpp:43), so the walk is unconditional; a flag word
// that stops being all ones fails here instead of silently shifting every
// later field.
func parseUserInfoBody(t *testing.T, data []byte) userInfoFields {
	t.Helper()
	r := &protocol.Reader{Data: data}
	var f userInfoFields
	f.LowFlags = uint32(r.U32())
	f.HighFlags = uint32(r.U32())
	if f.LowFlags != 0xFFFFFFFF || f.HighFlags != 0xFFFFFFFF {
		t.Fatalf("user info flags %#x/%#x, want all ones", f.LowFlags, f.HighFlags)
	}
	r.U8() // nameplate type marker
	f.NameplateID = uint32(r.U32())
	f.GameName = r.CString()
	f.GameName2 = r.CString()
	r.Bytes(3)
	f.Level = uint32(r.U32())
	r.U8() // UFLAG_LOW_UNK4
	f.Exp = binary.LittleEndian.Uint64(r.Bytes(8))
	f.Cash = binary.LittleEndian.Uint64(r.Bytes(8))
	f.Points = binary.LittleEndian.Uint64(r.Bytes(8))
	f.Battles = uint32(r.U32())
	f.Win = uint32(r.U32())
	f.Kills = uint32(r.U32())
	f.Deaths = uint32(r.U32())
	r.Bytes(16 * 4)
	r.CString() // region
	r.U16()
	r.U16()
	r.U16()
	r.CString() // PC cafe
	r.U32()     // UFLAG_LOW_CASH2
	r.U8()      // UFLAG_LOW_UNK11
	f.ClanID = uint32(r.U32())
	f.ClanMarkID = uint32(r.U32())
	f.ClanName = r.CString()
	r.Bytes(3)
	f.Tournament = r.U8()
	r.U8() // rank present marker
	r.Bytes(4)
	r.U8() // league
	r.Bytes(3)
	r.U16() // password boxes
	r.U32() // UFLAG_LOW_UNK17
	f.PrefixID = r.U16()
	r.Bytes(2)
	f.HonorPoints = uint32(r.U32())
	f.Bitmap = append([]byte{}, r.Bytes(128)...)
	r.Bytes(10)
	r.U8()
	r.U32()
	for i := range f.Titles {
		f.Titles[i] = r.U16()
	}
	r.U8()
	r.U16()
	f.Mileage = uint32(r.U32())
	r.U32()
	f.CharacterID = uint32(r.U32())
	r.U8()
	r.U8()
	r.Bytes(7 * 4)
	r.Bytes(2)
	r.Bytes(2)
	f.ChatColorID = r.U16()
	if r.Err != nil {
		t.Fatalf("user info truncated after %d bytes: %v", r.Pos, r.Err)
	}
	f.Consumed = r.Pos
	return f
}

// parseUserInfoPacket parses a user info packet, which prefixes the block with
// the character id.
func parseUserInfoPacket(t *testing.T, payload []byte) userInfoFields {
	t.Helper()
	r := &protocol.Reader{Data: payload}
	r.U32()
	return parseUserInfoBody(t, payload[4:])
}
