package store

import (
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"path/filepath"
	"sync"
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, e := Open(filepath.Join(t.TempDir(), "test.db3"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
func TestAccountPasswordHash(t *testing.T) {
	s := testStore(t)
	if e := s.Register("alice", "secret1", "127.0.0.1", 3); e != nil {
		t.Fatal(e)
	}
	var h string
	if e := s.DB.QueryRow("SELECT password FROM User WHERE userID=1").Scan(&h); e != nil || h == "secret1" {
		t.Fatal("password stored in clear", e)
	}
	if _, e := s.Login("alice", "wrong", "127.0.0.1", nil); e == nil {
		t.Fatal("bad password accepted")
	}
	u, e := s.Login("alice", "secret1", "127.0.0.1", nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CreateCharacter(u.ID, "Alice", config.DefaultUser{Level: 1, Points: 100}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("UPDATE User SET password='legacy' WHERE userID=?", u.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Login("alice", "legacy", "127.0.0.1", nil); e == nil {
		t.Fatal("plaintext password accepted")
	}
}
func TestConcurrentRegistrationIsUnique(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Register("alice", "secret1", "127.0.0.1", 0) == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 1 {
		t.Fatal("successful registrations", ok)
	}
}
func TestPurchaseRollbackAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db3")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Register("alice", "secret1", "127.0.0.1", 0); e != nil {
		t.Fatal(e)
	}
	if e = s.CreateCharacter(1, "Alice", config.DefaultUser{Level: 1, Points: 100}); e != nil {
		t.Fatal(e)
	}
	items := []Reward{{ID: 12, Count: 1}, {ID: 14, Count: 2}}
	if e = s.Purchase(1, true, 50, 0, items, 2, 0); e == nil {
		t.Fatal("full inventory purchase accepted")
	}
	ch, e := s.Character(1)
	if e != nil || ch.Points != 100 {
		t.Fatal(ch, e)
	}
	if _, e = s.DB.Exec("CREATE TRIGGER fail_second BEFORE INSERT ON UserInventory WHEN NEW.itemID=14 BEGIN SELECT RAISE(ABORT,'test rollback'); END"); e != nil {
		t.Fatal(e)
	}
	if e = s.Purchase(1, true, 50, 0, items, 100, 0); e == nil {
		t.Fatal("failed insert accepted")
	}
	ch, _ = s.Character(1)
	inv, _ := s.Inventory(1)
	if ch.Points != 100 || len(inv) != 1 {
		t.Fatal("partial purchase committed", ch, len(inv))
	}
	s.DB.Exec("DROP TRIGGER fail_second")
	if e = s.Purchase(1, true, 50, 5, items, 100, 0); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	ch, e = s.Character(1)
	if e != nil || ch.Points != 55 {
		t.Fatal(ch, e)
	}
	inv, e = s.Inventory(1)
	if e != nil || len(inv) != 3 {
		t.Fatal(inv, e)
	}
}
