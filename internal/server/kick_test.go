package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestSuperRoomHostKick(t *testing.T) {
	host := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "Host"}, room: 1, queue: make(chan outbound, 32)}
	guest := &client{user: store.User{ID: 2}, character: store.Character{ID: 2, Name: "Guest"}, room: 1, queue: make(chan outbound, 32)}
	var st settings
	st.fields[58] = []byte{1}
	r := &room{id: 1, host: host, members: []*client{host, guest}, settings: st}
	s := &Server{
		rooms:   map[int]*room{1: r},
		users:   map[int]*client{1: host, 2: guest},
		clients: map[*client]bool{host: true, guest: true},
	}
	data := []byte{1, 2, 0, 0, 0, 0, 0}
	if err := s.dispatch(host, protocol.Packet{ID: 112, Data: data}); err != nil {
		t.Fatal(err)
	}
	if guest.room != 0 || len(r.members) != 1 || r.members[0] != host {
		t.Fatalf("guest was not removed from room: room=%d members=%d", guest.room, len(r.members))
	}
	if !r.kicked[2] {
		t.Fatal("kicked guest was not recorded")
	}
	msg := <-host.queue
	if msg.id != 112 || len(msg.data) != 6 || msg.data[0] != 3 {
		t.Fatalf("unexpected kick broadcast %d %v", msg.id, msg.data)
	}
	msg = <-guest.queue
	if msg.id != 112 || len(msg.data) != 6 || msg.data[0] != 3 {
		t.Fatalf("guest did not receive kick packet %d %v", msg.id, msg.data)
	}
}
