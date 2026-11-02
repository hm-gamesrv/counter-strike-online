package store

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func TestGMUsersBansAndPoints(t *testing.T) {
	s := testStore(t)
	if err := s.Register("gmuser", "secret1", "10.0.0.7", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(1, "GmUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if exists, err := s.UserExists(1); err != nil || !exists {
		t.Fatalf("existing user reported missing: %v %v", exists, err)
	}
	if exists, err := s.UserExists(99); err != nil || exists {
		t.Fatalf("missing user reported present: %v %v", exists, err)
	}
	if id, err := s.UserIDByName("GMUSER"); err != nil || id != 1 {
		t.Fatalf("case-insensitive lookup returned %d, %v", id, err)
	}
	if _, err := s.UserIDByName("nobody"); err == nil {
		t.Fatal("missing login name resolved")
	}

	hwid := []byte("0123456789abcdef")
	if _, err := s.Login("gmuser", "secret1", "10.0.0.7", hwid); err != nil {
		t.Fatal(err)
	}
	if ip, err := s.UserLastIP(1); err != nil || ip != "10.0.0.7" {
		t.Fatalf("last IP %q, %v", ip, err)
	}
	if got, err := s.UserLastHWID(1); err != nil || string(got) != string(hwid) {
		t.Fatalf("last HWID %x, %v", got, err)
	}

	if points, err := s.AddPoints(1, 250); err != nil || points != 250 {
		t.Fatalf("points %d, %v", points, err)
	}
	character, err := s.Character(1)
	if err != nil || character.Points != 250 {
		t.Fatalf("stored points %d, %v", character.Points, err)
	}
	if _, err = s.DB.Exec("UPDATE UserCharacterExtended SET gameMaster=1 WHERE userID=1"); err != nil {
		t.Fatal(err)
	}
	if character, err = s.Character(1); err != nil || !character.GameMaster {
		t.Fatalf("game master flag not loaded: %+v, %v", character, err)
	}

	now := nowMinutes()
	if err := s.SetUserBan(1, 1, "cheating", 1, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login("gmuser", "secret1", "10.0.0.7", hwid); err == nil {
		t.Fatal("banned account logged in")
	}
	// A second ban updates the existing row instead of failing on the key.
	if err := s.SetUserBan(1, 2, "still cheating", 2, now); err != nil {
		t.Fatal(err)
	}
	var banType, term int
	var reason string
	if err = s.DB.QueryRow("SELECT type,reason,term FROM UserBan WHERE userID=1").Scan(&banType, &reason, &term); err != nil {
		t.Fatal(err)
	}
	if banType != 2 || reason != "still cheating" || term != now+2*24*60 {
		t.Fatalf("ban row %d %q %d", banType, reason, term)
	}
	if err := s.ClearUserBan(1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login("gmuser", "secret1", "10.0.0.7", hwid); err != nil {
		t.Fatalf("unbanned account rejected: %v", err)
	}

	// A zero-day term means effectively permanent, matching upstream /ban.
	if err := s.SetUserBan(1, 1, "permanent", 0, now); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRow("SELECT term FROM UserBan WHERE userID=1").Scan(&term); err != nil {
		t.Fatal(err)
	}
	if term != now+999999*24*60 {
		t.Fatalf("permanent term %d", term)
	}

	if err := s.AddIPBan("10.0.0.7"); err != nil {
		t.Fatal(err)
	}
	if err := s.AddIPBan("10.0.0.7"); err != nil {
		t.Fatalf("duplicate IP ban: %v", err)
	}
	if banned, err := s.IPBanned("10.0.0.7"); err != nil || !banned {
		t.Fatalf("IP ban lookup %v, %v", banned, err)
	}
	if banned, err := s.IPBanned("10.0.0.8"); err != nil || banned {
		t.Fatalf("unrelated IP banned %v, %v", banned, err)
	}
	if err := s.RemoveIPBan("10.0.0.7"); err != nil {
		t.Fatal(err)
	}
	if banned, err := s.IPBanned("10.0.0.7"); err != nil || banned {
		t.Fatalf("IP ban survived removal %v, %v", banned, err)
	}

	if err := s.AddHWIDBan(hwid); err != nil {
		t.Fatal(err)
	}
	if err := s.AddHWIDBan(hwid); err != nil {
		t.Fatalf("duplicate HWID ban: %v", err)
	}
	if banned, err := s.HWIDBanned(hwid); err != nil || !banned {
		t.Fatalf("HWID ban lookup %v, %v", banned, err)
	}
	if banned, err := s.HWIDBanned([]byte("fedcba9876543210")); err != nil || banned {
		t.Fatalf("unrelated HWID banned %v, %v", banned, err)
	}
	if err := s.RemoveHWIDBan(hwid); err != nil {
		t.Fatal(err)
	}
	if banned, err := s.HWIDBanned(hwid); err != nil || banned {
		t.Fatalf("HWID ban survived removal %v, %v", banned, err)
	}
}
