package server

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// TestHostLeaveForfeitsReward mirrors the reference: leaving a room mid-match
// drops that player's match stat and promotes a new host, so the leaver is not
// paid and the remaining players continue the match.
func TestHostLeaveForfeitsReward(t *testing.T) {
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

	s.leaveRoom(host)

	if r.match == nil {
		t.Fatal("match ended when the host left")
	}
	if _, ok := r.match.stats[1]; ok {
		t.Fatal("the leaver kept its match stat")
	}
	if r.host != guest {
		t.Fatal("host was not migrated to the remaining player")
	}
	// SettleMatch creates its table on first use; the leaver must not reach it.
	var tables, rows int
	if err = db.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='GoMatchSettlement'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 0 {
		if err = db.DB.QueryRow("SELECT count(*) FROM GoMatchSettlement WHERE matchID='leave-match'").Scan(&rows); err != nil {
			t.Fatal(err)
		}
	}
	if rows != 0 {
		t.Fatalf("settlement rows = %d", rows)
	}
}
