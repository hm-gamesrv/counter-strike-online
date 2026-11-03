package store

// Probe: code-review/probes/review-75cddae-store/store_test.go
//
// Injected with:
//   go test -overlay code-review/probes/review-75cddae-store/overlay.json ./internal/store -run '^TestStoreProbe' -count=1
//
// Every test here is additive: it asserts a store-layer invariant that the
// repository suite does not already pin. A failure is the finding.

import (
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func storeProbeDB(t *testing.T, users int) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "probe.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for i := 0; i < users; i++ {
		name := "probe" + strconv.Itoa(i)
		if err = s.Register(name, "secret1", "127.0.0.1", 0); err != nil {
			t.Fatal(err)
		}
		if err = s.CreateCharacter(i+1, "Probe"+strconv.Itoa(i), config.DefaultUser{Level: 1, Points: 1000}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

// storeProbeItem inserts one inventory row without going through the reward
// pipeline, so a test can pick the exact slot/expiry the path under test needs.
func storeProbeItem(t *testing.T, s *Store, user, slot, itemID, count, status, inUse, expiry int) {
	t.Helper()
	_, err := s.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,paintIDList,partSlot1,partSlot2,lockStatus) VALUES(?,?,?,?,?,?,?,?,0,0,0,0,0,'',0,0,0)", user, slot, itemID, count, status, inUse, nowMinutes(), expiry)
	if err != nil {
		t.Fatal(err)
	}
}

// storeProbeAbort installs a BEFORE trigger that aborts the write under test.
func storeProbeAbort(t *testing.T, s *Store, ddl string) {
	t.Helper()
	if _, err := s.DB.Exec("DROP TRIGGER IF EXISTS probe_fail"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Exec("DROP TRIGGER IF EXISTS probe_fail") })
}

func storeProbeCount(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A1. OpenItemBox consumes the key stack and grants the bundle in one
// transaction; a failure while granting must put the keys back.
func TestStoreProbeOpenItemBoxRollback(t *testing.T) {
	s := storeProbeDB(t, 1)
	storeProbeItem(t, s, 1, 2, 676, 5, 1, 0, 0)
	storeProbeAbort(t, s, "CREATE TRIGGER probe_fail BEFORE INSERT ON UserInventory WHEN NEW.itemID=14 BEGIN SELECT RAISE(ABORT,'probe'); END")
	awards := []Reward{
		{
			ID:    14,
			Count: 1,
		},
		{
			ID:    15,
			Count: 1,
		},
	}
	if _, err := s.OpenItemBox(1, 2, 676, 2, awards, 100, 0); err == nil {
		t.Fatal("failed grant accepted: key consumption committed without the bundle")
	}
	if n := storeProbeCount(t, s, "SELECT count FROM UserInventory WHERE userID=1 AND slot=2"); n != 5 {
		t.Fatalf("key stack after rollback = %d, want 5 (partial commit)", n)
	}
}

// A2. ExtendItem decrements the extender stack and rewrites the target expiry
// together; a rejected target update must not eat the extender.
func TestStoreProbeExtendItemRollback(t *testing.T) {
	s := storeProbeDB(t, 1)
	target := nowMinutes() + 1000
	storeProbeItem(t, s, 1, 3, 12, 1, 1, 1, target)
	storeProbeItem(t, s, 1, 4, 676, 3, 1, 0, 0)
	storeProbeAbort(t, s, "CREATE TRIGGER probe_fail BEFORE UPDATE ON UserInventory WHEN NEW.slot=3 BEGIN SELECT RAISE(ABORT,'probe'); END")
	if _, err := s.ExtendItem(1, 3, 676, 2); err == nil {
		t.Fatal("failed target update accepted")
	}
	if n := storeProbeCount(t, s, "SELECT count FROM UserInventory WHERE userID=1 AND slot=4"); n != 3 {
		t.Fatalf("extender stack after rollback = %d, want 3 (consumed without extending)", n)
	}
	if n := storeProbeCount(t, s, "SELECT expiryDate FROM UserInventory WHERE userID=1 AND slot=3"); n != target {
		t.Fatalf("target expiry changed to %d, want %d", n, target)
	}
}

// A3. EnhanceItem deletes the materials before it writes the target row; an
// aborted target write must restore every material.
func TestStoreProbeEnhanceMaterialRollback(t *testing.T) {
	s := storeProbeDB(t, 1)
	storeProbeItem(t, s, 1, 3, 12, 1, 1, 1, 0)
	storeProbeItem(t, s, 1, 4, 13, 1, 1, 0, 0)
	storeProbeAbort(t, s, "CREATE TRIGGER probe_fail BEFORE UPDATE ON UserInventory WHEN NEW.slot=3 BEGIN SELECT RAISE(ABORT,'probe'); END")
	if _, err := s.EnhanceItem(1, 3, []int{4}, 1, 0, 10); err == nil {
		t.Fatal("failed enhancement accepted")
	}
	if n := storeProbeCount(t, s, "SELECT count(*) FROM UserInventory WHERE userID=1 AND slot=4"); n != 1 {
		t.Fatalf("material rows after rollback = %d, want 1 (material consumed by a failed enhance)", n)
	}
}

// A4. The expiry sweep deletes rows and queues notices in one transaction; a
// failed notice insert must not delete the item.
func TestStoreProbeExpirySweepRollback(t *testing.T) {
	s := storeProbeDB(t, 1)
	expired := nowMinutes() - 10
	storeProbeItem(t, s, 1, 5, 12, 1, 1, 0, expired)
	storeProbeAbort(t, s, "CREATE TRIGGER probe_fail BEFORE INSERT ON UserExpiryNotice BEGIN SELECT RAISE(ABORT,'probe'); END")
	if _, _, err := s.ExpireItemsChanged(nowMinutes()); err == nil {
		t.Fatal("failed notice insert accepted")
	}
	if n := storeProbeCount(t, s, "SELECT count(*) FROM UserInventory WHERE userID=1 AND slot=5"); n != 1 {
		t.Fatalf("expired row after rollback = %d, want 1 (item deleted without a notice)", n)
	}
}

// A5. A multi-user settlement is all-or-nothing: a failure on the second
// participant must not pay the first one, and must not burn the match id.
func TestStoreProbeSettlementSecondParticipantRollback(t *testing.T) {
	s := storeProbeDB(t, 2)
	storeProbeAbort(t, s, "CREATE TRIGGER probe_fail BEFORE UPDATE ON UserCharacter WHEN NEW.userID=2 BEGIN SELECT RAISE(ABORT,'probe'); END")
	rewards := []MatchReward{
		{
			UserID:      1,
			Exp:         250,
			Points:      200,
			Kills:       2,
			Deaths:      1,
			UpdateStats: true,
		},
		{
			UserID:      2,
			Exp:         250,
			Points:      200,
			Kills:       2,
			Deaths:      1,
			UpdateStats: true,
		},
	}
	if err := s.SettleMatch("probe-match", rewards); err == nil {
		t.Fatal("failed second participant accepted")
	}
	first, err := s.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Exp != 0 || first.Points != 1000 || first.Kills != 0 {
		t.Fatalf("first participant paid by a failed settlement: exp=%d points=%d kills=%d", first.Exp, first.Points, first.Kills)
	}
	// The rollback also removed the GoMatchSettlement row, so the same match id
	// has to be retryable once the cause is gone. That is the property the
	// durable (matchID,userID) key is for.
	if _, err = s.DB.Exec("DROP TRIGGER IF EXISTS probe_fail"); err != nil {
		t.Fatal(err)
	}
	if err = s.SettleMatch("probe-match", rewards); err != nil {
		t.Fatalf("retry after a rolled-back settlement was rejected: %v (the failed attempt burned the match id)", err)
	}
	first, err = s.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Exp != 250 || first.Points != 1200 {
		t.Fatalf("retry after rollback: exp=%d points=%d, want 250/1200 (paid twice or not at all)", first.Exp, first.Points)
	}
}

// A6. ClearExpiryNotices removes exactly the delivered rows and is replayable:
// the sender retries the same identity list after a write timeout.
func TestStoreProbeClearExpiryNoticesIdentityAndReplay(t *testing.T) {
	s := storeProbeDB(t, 1)
	for i := 0; i < 3; i++ {
		if _, err := s.DB.Exec("INSERT INTO UserExpiryNotice(userID,itemID) VALUES(1,?)", 100+i); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.ExpiryNoticeRows(1)
	if err != nil || len(rows) != 3 {
		t.Fatalf("queued notices = %d, %v", len(rows), err)
	}
	delivered := []int{rows[0].RowID, rows[2].RowID}
	for i := 0; i < 2; i++ {
		if err = s.ClearExpiryNotices(1, delivered); err != nil {
			t.Fatalf("replay %d failed: %v", i, err)
		}
	}
	left, err := s.ExpiryNoticeRows(1)
	if err != nil || len(left) != 1 || left[0].ItemID != 101 {
		t.Fatalf("undelivered notice lost: %v, %v", left, err)
	}
}

// A7. GrantRewardNotice consumes one offline notice and pays once: a replay of
// the same reward id must not pay a second time.
func TestStoreProbeGrantRewardNoticeReplay(t *testing.T) {
	s := storeProbeDB(t, 1)
	if _, err := s.DB.Exec("INSERT INTO UserRewardNotice(userID,rewardID) VALUES(1,3000)"); err != nil {
		t.Fatal(err)
	}
	g := Grant{
		Items: []Reward{
			{
				ID:    14,
				Count: 1,
			},
		},
	}
	if _, err := s.GrantRewardNotice(1, 3000, g, 100, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GrantRewardNotice(1, 3000, g, 100, 0); !errors.Is(err, ErrRewardClaimed) {
		t.Fatalf("replayed notice returned %v, want ErrRewardClaimed", err)
	}
	if n := storeProbeCount(t, s, "SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=14"); n != 1 {
		t.Fatalf("notice paid %d times, want 1", n)
	}
}

// C1. SetTitle is a read-modify-write on one serialized column with no
// transaction (title.go:56 .. :66). Two writers for different slots interleave
// between the SELECT and the UPDATE and one slot loses its value. The rest of
// the store wraps this shape in s.DB.Begin(); this path does not.
func TestStoreProbeSetTitleLostUpdate(t *testing.T) {
	s := storeProbeDB(t, 1)
	const rounds = 2000
	lost := 0
	var firstLost int
	var example string
	for round := 0; round < rounds && lost < 20; round++ {
		if _, err := s.DB.Exec("UPDATE UserCharacter SET titles='0,0,0,0,0' WHERE userID=1"); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.SetTitle(1, 0, 101)
		}()
		go func() {
			defer wg.Done()
			s.SetTitle(1, 1, 202)
		}()
		wg.Wait()
		ch, err := s.Character(1)
		if err != nil {
			t.Fatal(err)
		}
		if ch.Titles[0] != 101 || ch.Titles[1] != 202 {
			lost++
			if firstLost == 0 {
				firstLost = round
				example = strconv.Itoa(ch.Titles[0]) + "," + strconv.Itoa(ch.Titles[1])
			}
		}
	}
	if lost != 0 {
		t.Fatalf("SetTitle lost an update in %d/%d rounds (first at round %d, titles=[%s], want [101,202,...]): the read-modify-write is not transactional", lost, rounds, firstLost+1, example)
	}
	t.Logf("no lost update observed in %d rounds; the window is timing dependent", rounds)
}

// C2. ToggleNameplate/ToggleEffectItem resolve the row to activate by slot
// alone (effect.go:20 activateExpiry) instead of through readInventory, so it is
// the only "mutate the item I was pointed at" path in the package that does not
// apply liveItemSQL. A row that expired after the caller listed the bag - or one
// a modified client addresses directly - is revived as status=1,inUse=1 and its
// item id is written to UserCharacter.nameplateID, while readInventory still
// hides the row. The account ends up wearing a nameplate it has no live copy of.
func TestStoreProbeToggleNameplateAcceptsExpiredRow(t *testing.T) {
	s := storeProbeDB(t, 1)
	expired := nowMinutes() - 10
	storeProbeItem(t, s, 1, 6, 7001, 1, 1, 0, expired)
	visible, err := s.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range visible {
		if item.Slot == 6 {
			t.Fatalf("setup: expired row is visible in the bag: %+v", item)
		}
	}
	_, nameplate, err := s.ToggleNameplate(1, 7001, 6)
	if err == nil {
		ch, cerr := s.Character(1)
		if cerr != nil {
			t.Fatal(cerr)
		}
		t.Fatalf("expired row activated: ToggleNameplate returned nameplate=%d and UserCharacter.nameplateID=%d for an item the bag no longer contains", nameplate, ch.NameplateID)
	}
}
