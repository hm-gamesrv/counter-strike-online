package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func socialFixture() (*Server, *room, *client, *client, *client) {
	users := map[int]*client{}
	clients := map[*client]bool{}
	for i, name := range []string{"Host", "Guest", "Other"} {
		u := &client{user: store.User{ID: i + 1, Name: name}, character: store.Character{ID: i + 1, Name: name}, queue: make(chan outbound, 64), room: 1}
		users[i+1] = u
		clients[u] = true
	}
	r := &room{id: 1, host: users[1], members: []*client{users[1], users[2], users[3]}}
	r.settings.setNumber(8, 4, 1)
	s := &Server{users: users, clients: clients, rooms: map[int]*room{1: r}}
	return s, r, users[1], users[2], users[3]
}

func TestRoomKickPermissionsAndHostChange(t *testing.T) {
	s, r, host, guest, other := socialFixture()
	s.kickRoomUser(guest, host.user.ID)
	s.kickRoomUser(host, host.user.ID)
	host.playing = true
	s.kickRoomUser(host, guest.user.ID)
	if len(r.members) != 3 || len(r.kicked) != 0 {
		t.Fatal("unauthorized kick changed membership")
	}
	host.playing = false
	r.match = &gameMatch{}
	s.kickRoomUser(host, guest.user.ID)
	if len(r.members) != 3 || len(r.kicked) != 0 {
		t.Fatal("host outside combat kicked a member during an active match")
	}
	r.match = nil
	s.kickRoomUser(host, guest.user.ID)
	if guest.room != 0 || !guest.inLobby || len(r.members) != 2 || !r.kicked[guest.user.ID] {
		t.Fatal("kick did not return member to lobby or remember exclusion")
	}
	w := &protocol.Writer{}
	w.U8(23)
	w.U8(0)
	w.U16(r.id)
	w.String("")
	if err := s.roomRequest(guest, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	if guest.room != 0 || len(r.members) != 2 {
		t.Fatal("kicked member rejoined")
	}
	s.leaveRoom(host)
	if r.host != other || len(r.kicked) != 0 {
		t.Fatal("host transfer should clear previous host's kick list")
	}
}

func TestRoomInvitesParseBeforeSendingAndDeduplicate(t *testing.T) {
	s, _, host, guest, other := socialFixture()
	invite := func(count int, names ...string) *protocol.Reader {
		w := &protocol.Writer{}
		w.String("join the test")
		w.U16(count)
		for _, name := range names {
			w.String(name)
		}
		return &protocol.Reader{Data: w.Data}
	}
	if err := s.inviteRoomUsers(host, invite(2, "Guest")); err == nil || len(guest.queue) != 0 {
		t.Fatal("truncated invitation delivered a partial request")
	}
	if err := s.inviteRoomUsers(host, invite(257)); err == nil {
		t.Fatal("unbounded invitation list accepted")
	}
	if err := s.inviteRoomUsers(host, invite(4, "Host", "Guest", "Guest", "Offline")); err != nil {
		t.Fatal(err)
	}
	if len(guest.queue) != 1 || len(host.queue) != 0 || len(other.queue) != 0 {
		t.Fatal("invitation delivered to wrong recipients or duplicated")
	}
	msg := <-guest.queue
	p := &protocol.Reader{Data: msg.data}
	if msg.id != 80 || p.U8() != 3 || p.U8() != 1 || p.U8() != 1 || p.U16() != 1 {
		t.Fatal("incorrect invitation destination")
	}
	p.Bytes(10)
	if p.CString() != "228 unk str" || p.CString() != "Host" || p.CString() != "join the test" || p.Err != nil || p.Pos != len(p.Data) {
		t.Fatal("incorrect invitation payload", p.Err)
	}
	guest.inLobby = true
	s.roomInviteList(host)
	msg = <-host.queue
	p = &protocol.Reader{Data: msg.data}
	if msg.id != 65 || p.U8() != 12 || p.U16() != 1 || p.U32() != guest.user.ID {
		t.Fatal("invitation list should contain lobby members only")
	}
}
