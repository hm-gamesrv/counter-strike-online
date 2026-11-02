package server

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// TestHostLeaveSettlesMatch covers the scenario room path: the client closes
// its own result window and asks to leave the room instead of sending the host
// game-end message, so the reward has to be paid while leaving.
func TestHostLeaveSettlesMatch(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "leave.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for i, name := range []string{"hostuser", "guestuser"} {
		if err = db.Register(name, "secret1", "127.0.0.1", 0); err != nil {
			t.Fatal(err)
		}
		if err = db.CreateCharacter(i+1, name, config.DefaultUser{Level: 1}); err != nil {
			t.Fatal(err)
		}
	}
	host := pipeClient(t, 1, "HostUser")
	guest := pipeClient(t, 2, "GuestUser")
	s := &Server{
		Store: db,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		users: map[int]*client{
			1: host,
			2: guest,
		},
		clients: map[*client]bool{
			host:  true,
			guest: true,
		},
		rooms: map[int]*room{},
	}
	s.Config.GameMatch.CalcResults.GameModeCoefficient = map[string]struct {
		Exp    int
		Points int
	}{"15": {Exp: 5, Points: 2}}
	r := &room{
		id:      1,
		host:    host,
		members: []*client{host, guest},
		match:   &gameMatch{id: "leave-match", mode: 15, mapID: 235, stats: map[int]*matchStat{}},
	}
	host.room, guest.room = 1, 1
	host.playing, guest.playing = true, true
	r.match.stats[1] = &matchStat{user: host}
	r.match.stats[2] = &matchStat{user: guest}
	s.rooms[1] = r

	s.leaveRoomRequest(host)

	if r.match != nil {
		t.Fatal("match survived the host leaving")
	}
	if host.room != 0 {
		t.Fatal("host did not leave the room")
	}
	var rows int
	if err = db.DB.QueryRow("SELECT count(*) FROM GoMatchSettlement WHERE matchID='leave-match'").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("settlement rows = %d", rows)
	}
}
