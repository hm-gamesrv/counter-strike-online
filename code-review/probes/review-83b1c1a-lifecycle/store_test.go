package store

import (
	"sync"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

// Two accounts race for the same clan storage slot. The reference wraps the
// whole give/take in SQLite::Transaction (userdatabase_sqlite.cpp:4608), and the
// Go side keeps Store.DB at one connection, so exactly one take may succeed and
// the item must land in exactly one bag. This probe documents the invariant; it
// passes on the current code and is here so a future connection-pool change
// cannot silently reintroduce a double take.
func TestStoreProbeClanStorageConcurrentTake(t *testing.T) {
	s := testStore(t)
	ids := map[string]int{}
	for _, name := range []string{"master", "first", "second"} {
		if err := s.Register(name, "secret1", "127.0.0.1", 0); err != nil {
			t.Fatal(err)
		}
		var id int
		if err := s.DB.QueryRow("SELECT userID FROM User WHERE userName=?", name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[name] = id
		if err := s.CreateCharacter(id, name, config.DefaultUser{Level: 1, Points: 40000}); err != nil {
			t.Fatal(err)
		}
	}
	clanID, err := s.CreateClan(ClanCreate{MasterID: ids["master"], Name: "Race", JoinMethod: 3}, 1000)
	if err != nil || clanID <= 0 {
		t.Fatalf("create clan: %d %v", clanID, err)
	}
	for _, name := range []string{"first", "second"} {
		if code, _, err := s.JoinClan(ids[name], clanID, 1000); err != nil || code != 1 {
			t.Fatalf("join %s: %d %v", name, code, err)
		}
	}
	if _, err = s.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,40,270,1,1,0,?,0,0,0,0,0,0,0,0,0)", ids["master"], nowMinutes()); err != nil {
		t.Fatal(err)
	}
	if _, code, err := s.GiveClanItem(ids["master"], 0, 40); err != nil || code != 1 {
		t.Fatalf("give: %d %v", code, err)
	}
	codes := make([]int, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, name := range []string{"first", "second"} {
		wg.Add(1)
		go func(i int, userID int) {
			defer wg.Done()
			_, codes[i], errs[i] = s.TakeClanItem(userID, 0, 0, 3000, 0)
		}(i, ids[name])
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("take errors %v %v", errs[0], errs[1])
	}
	if !(codes[0] == 1 && codes[1] == -1 || codes[0] == -1 && codes[1] == 1) {
		t.Fatalf("take codes %v; exactly one caller may win", codes)
	}
	var rows int
	if err = s.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE itemID=270").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("the stored item was handed out %d times", rows)
	}
}

// A duplicate login takes the account over: lobby.go:247-264 calls endSession
// (which does not touch UserSession) and then StartSession, whose DELETE silently
// drops the previous row. The reference destroys the old CUser, whose destructor
// calls DropSession (user.cpp:32-34) and writes the elapsed session into
// UserSessionHistory (userdatabase_sqlite.cpp:452-465) before the new session row
// is inserted. Here the old segment leaves no history row at all.
func TestStoreProbeTakeoverDropsSessionHistory(t *testing.T) {
	s := testStore(t)
	if err := s.Register("takeover", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.StartSession(1, "10.0.0.1", nil, 1000); err != nil {
		t.Fatal(err)
	}
	// The previous connection is closed and the replacement inserts its session.
	if err := s.StartSession(1, "10.0.0.2", nil, 1010); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := s.DB.QueryRow("SELECT count(*) FROM UserSessionHistory WHERE userID=1").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("takeover left %d history rows for the superseded session; the reference writes one (DropSession on the old CUser)", rows)
	}
}
