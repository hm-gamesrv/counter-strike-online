package server

import (
	"net"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestEndSessionClosesStaleConnection(t *testing.T) {
	serverConn, peerConn := net.Pipe()
	defer peerConn.Close()
	old := &client{
		conn: serverConn,
		user: store.User{
			ID: 7,
		},
		queue: make(chan outbound, 1),
	}
	s := &Server{
		users: map[int]*client{
			7: old,
		},
	}
	if err := peerConn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	s.endSession(7)
	if s.users[7] != nil {
		t.Fatal("stale session was not removed")
	}
	if _, err := peerConn.Read(make([]byte, 1)); err == nil {
		t.Fatal("stale connection was not closed")
	}
}

func TestEndSessionIgnoresUnknownUser(t *testing.T) {
	s := &Server{
		users: map[int]*client{},
	}
	s.endSession(99)
	if len(s.users) != 0 {
		t.Fatalf("unexpected session count %d", len(s.users))
	}
}
