package server

import (
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func pipeClient(t *testing.T, id int, name string) *client {
	t.Helper()
	serverConn, peerConn := net.Pipe()
	t.Cleanup(func() {
		serverConn.Close()
		peerConn.Close()
	})
	return &client{
		conn: serverConn,
		user: store.User{
			ID:   id,
			Name: name,
		},
		character: store.Character{
			ID:   id,
			Name: name,
		},
		version: true,
		queue:   make(chan outbound, 32),
	}
}

func TestRestoreSessionRejoinsActiveMatch(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "reconnect.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("reconnectuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "ReconnectUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	host := pipeClient(t, 2, "HostUser")
	old := pipeClient(t, 1, "ReconnectUser")
	host.room = 1
	old.room = 1
	old.playing = true
	old.team = 1
	match := &gameMatch{
		id:   "match-1",
		mode: 4,
		stats: map[int]*matchStat{
			1: {
				user:    old,
				kills:   5,
				deaths:  2,
				score:   3,
				items:   []store.Item{{ID: 12, Count: 1, InUse: 1, Status: 1}},
				started: time.Now().Add(-time.Minute),
				questStreaks: map[[3]int]int{
					{1, 2, 3}: 1,
				},
			},
		},
	}
	settings := &settings{}
	settings.setNumber(8, 8, 1)
	currentRoom := &room{
		id:       1,
		host:     host,
		members:  []*client{host, old},
		settings: *settings,
		match:    match,
	}
	s := &Server{
		Store: db,
		Config: config.Config{
			InventorySlotMax: 10,
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		users: map[int]*client{
			2: host,
		},
		rooms: map[int]*room{
			1: currentRoom,
		},
		restore: map[int]reconnectSlot{},
	}
	s.stashReconnect(old)
	s.leaveRoom(old)

	reconnected := pipeClient(t, 1, "ReconnectUser")
	reconnected.inLobby = true
	s.users[1] = reconnected
	s.restoreSession(reconnected)

	if reconnected.room != 1 || !reconnected.playing {
		t.Fatalf("reconnected room=%d playing=%v", reconnected.room, reconnected.playing)
	}
	stat := currentRoom.match.stats[1]
	if stat == nil || stat.user != reconnected {
		t.Fatal("match stat was not restored to the new client")
	}
	if stat.kills != 5 || stat.deaths != 2 || stat.score != 3 {
		t.Fatalf("restored stat kills=%d deaths=%d score=%d", stat.kills, stat.deaths, stat.score)
	}
	if stat.questStreaks[[3]int{1, 2, 3}] != 1 {
		t.Fatal("quest streak state was not copied")
	}
	var sawMode, sawGame bool
	for len(reconnected.queue) > 0 {
		msg := <-reconnected.queue
		if msg.id == 180 {
			sawMode = true
		}
		if msg.id == 68 {
			sawGame = true
		}
	}
	if !sawMode || !sawGame {
		t.Fatalf("missing restore packets mode=%v game=%v", sawMode, sawGame)
	}
}

func TestRestoreSessionRejectsExpiredSlot(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "reconnect.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	roomOwner := pipeClient(t, 2, "HostUser")
	currentRoom := &room{
		id:      1,
		host:    roomOwner,
		members: []*client{roomOwner},
	}
	s := &Server{
		Store: db,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		rooms: map[int]*room{
			1: currentRoom,
		},
		restore: map[int]reconnectSlot{
			1: {
				roomID:  1,
				expires: time.Now().Add(-time.Second),
			},
		},
	}
	c := pipeClient(t, 1, "ReconnectUser")
	c.inLobby = true
	s.restoreSession(c)
	if c.room != 0 {
		t.Fatalf("expired restore joined room %d", c.room)
	}
	if len(currentRoom.members) != 1 {
		t.Fatalf("expired restore changed members %d", len(currentRoom.members))
	}
}
func TestEndSessionStashesRoomForReconnect(t *testing.T) {
	host := pipeClient(t, 2, "HostUser")
	old := pipeClient(t, 1, "ReconnectUser")
	host.room = 1
	old.room = 1
	old.playing = true
	old.team = 2
	match := &gameMatch{
		id:   "match-2",
		mode: 4,
		stats: map[int]*matchStat{
			1: {
				user:   old,
				kills:  7,
				deaths: 3,
			},
		},
	}
	settings := &settings{}
	settings.setNumber(8, 8, 1)
	currentRoom := &room{
		id:       1,
		host:     host,
		members:  []*client{host, old},
		settings: *settings,
		match:    match,
	}
	s := &Server{
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		users: map[int]*client{
			1: old,
			2: host,
		},
		rooms: map[int]*room{
			1: currentRoom,
		},
		restore: map[int]reconnectSlot{},
	}
	s.endSession(1)
	slot, ok := s.restore[1]
	if !ok {
		t.Fatal("endSession did not stash reconnect state")
	}
	if slot.roomID != 1 || slot.matchID != "match-2" || !slot.hasStat || slot.stat.kills != 7 {
		t.Fatalf("unexpected reconnect slot %+v", slot)
	}
	if s.users[1] != nil {
		t.Fatal("stale session still registered")
	}
	if old.room != 0 {
		t.Fatalf("stale client still in room %d", old.room)
	}
	if len(currentRoom.members) != 1 || currentRoom.members[0] != host {
		t.Fatalf("stale member not removed: %d", len(currentRoom.members))
	}
	if currentRoom.match.stats[1] != nil {
		t.Fatal("stale match stat not cleared")
	}
}
