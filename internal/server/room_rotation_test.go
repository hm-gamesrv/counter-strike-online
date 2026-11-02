package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func rotationSettings(ids []int, current int) settings {
	st := settings{}
	st.setNumber(7, current, 2)
	b := []byte{byte(len(ids))}
	for _, id := range ids {
		b = append(b, 0, byte(id), byte(id>>8))
	}
	st.fields[41] = b
	st.flags[1] |= 1 << 9
	return st
}

func TestRotateRoomMap(t *testing.T) {
	a := &client{queue: make(chan outbound, 16)}
	r := &room{id: 1, members: []*client{a}, settings: rotationSettings([]int{1, 4, 7}, 1)}
	s := &Server{}
	if !s.rotateRoomMap(r, false) {
		t.Fatal("rotation failed")
	}
	if r.settings.number(7) != 4 || r.settings.number(48) != 4 || r.settings.number(42) != 2 {
		t.Fatalf("next map %d map2 %d index %d", r.settings.number(7), r.settings.number(48), r.settings.number(42))
	}
	message := <-a.queue
	reader := &protocol.Reader{Data: message.data}
	if message.id != 65 || reader.U8() != 4 || reader.U32() != 1<<7 || reader.U32() != 1<<10|1<<16 {
		t.Fatalf("rotation packet %v", message.data)
	}
	reader.U32()
	reader.U32()
	if reader.U16() != 4 || reader.U8() != 2 || reader.U16() != 4 {
		t.Fatalf("rotation payload %v", message.data)
	}

	if !s.rotateRoomMap(r, false) || r.settings.number(7) != 7 || r.settings.number(42) != 3 {
		t.Fatalf("second rotation %d %d", r.settings.number(7), r.settings.number(42))
	}
	if !s.rotateRoomMap(r, false) || r.settings.number(7) != 1 || r.settings.number(42) != 1 {
		t.Fatalf("wrap rotation %d %d", r.settings.number(7), r.settings.number(42))
	}

	r.settings.setNumber(7, 7, 2)
	if !s.rotateRoomMap(r, true) || r.settings.number(7) != 1 || r.settings.number(42) != 1 {
		t.Fatalf("forced rotation %d %d", r.settings.number(7), r.settings.number(42))
	}

	plain := &room{}
	if s.rotateRoomMap(plain, false) {
		t.Fatal("rotation without a playlist")
	}
}
