package store

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func TestSessionLifecycle(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "session.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("sessionuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "SessionUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if err = db.StartSession(1, "127.0.0.1", []byte{1, 2, 3}, 100); err != nil {
		t.Fatal(err)
	}
	var ip string
	var started int
	if err = db.DB.QueryRow("SELECT ip,sessionTime FROM UserSession WHERE userID=1").Scan(&ip, &started); err != nil {
		t.Fatal(err)
	}
	if ip != "127.0.0.1" || started != 100 {
		t.Fatalf("session ip=%s start=%d", ip, started)
	}
	if err = db.EndSession(1); err != nil {
		t.Fatal(err)
	}
	var rows, sessionDate, sessionTime int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserSession WHERE userID=1").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("active session still present %d", rows)
	}
	if err = db.DB.QueryRow("SELECT sessionDate,sessionTime FROM UserSessionHistory WHERE userID=1").Scan(&sessionDate, &sessionTime); err != nil {
		t.Fatal(err)
	}
	// The reference stamps the history row with User.lastLogonTime and keeps the
	// raw session start time (userdatabase_sqlite.cpp:457).
	if sessionDate != 0 || sessionTime != 100 {
		t.Fatalf("history date=%d time=%d", sessionDate, sessionTime)
	}
}

// TestStartSessionArchivesSupersededSession pins the takeover path: the second
// login replaces the live session, and the replaced row still lands in history.
func TestStartSessionArchivesSupersededSession(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "takeover.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("takeoveruser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.StartSession(1, "10.0.0.1", []byte{1, 2, 3}, 1000); err != nil {
		t.Fatal(err)
	}
	if err = db.StartSession(1, "10.0.0.2", []byte{4, 5, 6}, 1010); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserSessionHistory WHERE userID=1").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("takeover archived %d rows, want 1", rows)
	}
	var ip string
	var started int
	if err = db.DB.QueryRow("SELECT ip,sessionTime FROM UserSessionHistory WHERE userID=1").Scan(&ip, &started); err != nil {
		t.Fatal(err)
	}
	if ip != "10.0.0.1" || started != 1000 {
		t.Fatalf("archived session ip=%s start=%d", ip, started)
	}
	var live int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserSession WHERE userID=1").Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 1 {
		t.Fatalf("live sessions %d, want 1", live)
	}
}
