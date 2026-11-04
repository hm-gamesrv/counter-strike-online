package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func TestGMTournamentToggle(t *testing.T) {
	addr, db, _ := startGMTestServer(t, true)
	p := gmJoin(t, addr, "TourneyGM", []byte("0123456789abcdef"))
	p.command("/tournament")
	value := userInfoTournament(t, p)
	gmNoticeExact(t, p, 10, "Tournament HUD is ON")
	var stored int
	if e := db.DB.QueryRow("SELECT tournament FROM UserCharacter WHERE gameName=?", "TourneyGM").Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if stored != 0xFF {
		t.Fatalf("tournament flag %d", stored)
	}
	if value != 0xFF {
		t.Fatalf("user info tournament byte %d", value)
	}
	p.command("/tournament")
	gmNoticeExact(t, p, 10, "Tournament HUD is OFF")
	if e := db.DB.QueryRow("SELECT tournament FROM UserCharacter WHERE gameName=?", "TourneyGM").Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if stored != 0 {
		t.Fatalf("tournament flag after toggle off %d", stored)
	}
}

// userInfoTournament reads the value the 157 update puts after the clan block,
// skipping the user info packet the login sequence already queued.
func userInfoTournament(t *testing.T, p *testPeer) int {
	t.Helper()
	// Every 157 now carries the tournament byte (the reference sets every flag
	// bit), so wait for the update that actually turns the HUD on instead of
	// taking the value the login sequence queued.
	for i := 0; i < 20; i++ {
		if value := parseUserInfoPacket(t, p.next(157).Data).Tournament; value != 0 {
			return value
		}
	}
	t.Fatal("no 157 update carries the tournament flag")
	return 0
}

func TestGMRequestSnapshotAndGhostRoom(t *testing.T) {
	addr, _, srv := startGMTestServer(t, true)
	p := gmJoin(t, addr, "GhostGM", []byte("fedcba9876543210"))
	p.command("/reqsnapshot")
	gmNoticeExact(t, p, 11, "Not implemented")
	w := &protocol.Writer{}
	w.U8(1)
	w.U8(1)
	p.send(7, w)
	gmSync(t, p)
	p.command("/addgroom")
	gmSync(t, p)
	srv.mu.Lock()
	rooms := len(srv.rooms)
	name := ""
	for _, room := range srv.rooms {
		name = room.settings.text(0)
	}
	srv.mu.Unlock()
	if rooms != 1 {
		t.Fatalf("rooms after /addgroom: %d", rooms)
	}
	if name != "GHOST ROOM NAME" {
		t.Fatalf("ghost room name %q", name)
	}
}

func TestMetadataHashPacketIsEmpty(t *testing.T) {
	dir := t.TempDir()
	if e := assets.Init(dir); e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{Metadata: map[string]bool{"WeaponProp": true, "Hash": true}}
	packets, e := loadMetadata(dir, cfg)
	if e != nil {
		t.Fatal(e)
	}
	if len(packets) != 2 {
		t.Fatalf("packets %d", len(packets))
	}
	if packets[0][0] != 42 {
		t.Fatalf("first metadata type %d, want WeaponProp", packets[0][0])
	}
	if len(packets[1]) != 0 {
		t.Fatalf("hash packet carries %d bytes; the baseline sends none", len(packets[1]))
	}
}
