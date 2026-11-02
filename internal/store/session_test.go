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
	if err = db.EndSession(1, 160); err != nil {
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
	if sessionDate != 160 || sessionTime != 60 {
		t.Fatalf("history date=%d time=%d", sessionDate, sessionTime)
	}
}
