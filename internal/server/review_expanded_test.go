package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// TestExpiryNoticesKeepUnsentRows: the client takes at most 255 entries, so a
// longer queue must survive for the next login instead of being wiped.
func TestExpiryNoticesKeepUnsentRows(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	if _, err := db.DB.Exec("WITH RECURSIVE n(x) AS (SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<260) INSERT INTO UserExpiryNotice(userID,itemID) SELECT 1,x FROM n"); err != nil {
		t.Fatal(err)
	}
	if err := s.sendExpiryNotices(c); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserExpiryNotice WHERE userID=1").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 5 {
		t.Fatalf("remaining notices = %d, want 5", remaining)
	}
	if msg := <-c.queue; msg.id != 67 || msg.data[0] != 62 || msg.data[1] != 255 {
		t.Fatalf("notice packet id=%d data=%v", msg.id, msg.data[:2])
	}
}

// TestOfflineRewardNoticeGrantedOnce keeps the notice and the award in one
// transaction, so a failed delete cannot pay the same reward twice.
func TestOfflineRewardNoticeGrantedOnce(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	if _, err := db.DB.Exec("INSERT INTO UserRewardNotice(userID,rewardID) VALUES(1,3000)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("CREATE TRIGGER failNotice BEFORE DELETE ON UserRewardNotice BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.grantRewardNotices(c); err == nil {
		t.Fatal("expected the injected delete failure")
	}
	if _, err := db.DB.Exec("DROP TRIGGER failNotice"); err != nil {
		t.Fatal(err)
	}
	if err := s.grantRewardNotices(c); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=12").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("offline reward granted %d times, want 1", count)
	}
}

// TestWeaponReleaseKeepsPrizeAfterBagFreed: the last letter and the prize share
// one transaction, so a full bag leaves the word retryable.
func TestWeaponReleaseKeepsPrizeAfterBagFreed(t *testing.T) {
	s, c, id := weaponReleaseServer(t, true)
	if _, err := s.Store.GrantReward(id, store.Grant{Items: []store.Reward{{ID: 567, Count: 1}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.DB.Exec("INSERT INTO UserMiniGameWeaponReleaseItemProgress(userID,slot,character,opened) VALUES(?,567,30,0)", id); err != nil {
		t.Fatal(err)
	}
	s.Config.InventorySlotMax = 1
	request := []byte{0, 4, 'G'}
	if err := s.weaponReleaseSet(c, &protocol.Reader{Data: request}); err != nil {
		t.Fatal(err)
	}
	var opened int
	if err := s.Store.DB.QueryRow("SELECT opened FROM UserMiniGameWeaponReleaseItemProgress WHERE userID=? AND slot=567", id).Scan(&opened); err != nil {
		t.Fatal(err)
	}
	if opened != 0 {
		t.Fatal("word was marked opened although the prize could not be granted")
	}
	s.Config.InventorySlotMax = 100
	if err := s.weaponReleaseSet(c, &protocol.Reader{Data: request}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.Store.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=? AND itemID=567", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("prizes after retry = %d, want 2", count)
	}
}

// TestEnhanceDrawsEveryAttribute: the reference draws from an inclusive 0..5
// range, so Ammo (index 5) must be reachable.
func TestEnhanceDrawsEveryAttribute(t *testing.T) {
	s := &Server{}
	var counts [7]int
	for i := 0; i < 1000; i++ {
		index, ok := s.enhanceAttribute([7]int{}, 0, [7]int{9, 9, 9, 9, 9, 9, 0})
		if !ok {
			t.Fatal("no eligible attribute")
		}
		counts[index]++
	}
	if counts[5] == 0 {
		t.Fatalf("Ammo never drawn: %v", counts)
	}
}

// TestRewardClaimSnapshotMatchesStoredItem: auto-equip rewrites status and
// expiry, so the 154 packet must carry the stored values, not the grant snapshot.
func TestRewardClaimSnapshotMatchesStoredItem(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "wire.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("wireuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "WireUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:  db,
		Config: config.Config{InventorySlotMax: 100},
		items: map[int]itemDefinition{
			471: {
				Category:  12,
				ClassName: "HeadCostume",
			},
		},
	}
	c := pipeClient(t, 1, "WireUser")
	p := pendingReward{
		Label: itemReward{Title: "Review"},
		Grant: store.Grant{
			Items: []store.Reward{
				{
					ID:       471,
					Count:    1,
					Duration: 3,
					Dormant:  true,
				},
			},
		},
	}
	payload, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	progress, err := db.RecordQuestProgress(1, 1, 1, 1, 1, 1, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.finishRewardClaim(c, progress.Token, 0); err != nil {
		t.Fatal(err)
	}
	var slot, expiry int
	if err = db.DB.QueryRow("SELECT slot,expiryDate FROM UserInventory WHERE userID=1 AND itemID=471").Scan(&slot, &expiry); err != nil {
		t.Fatal(err)
	}
	marker := &protocol.Writer{}
	marker.U16(slot)
	marker.U8(1)
	marker.U16(471)
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id != 154 {
			continue
		}
		at := bytes.Index(msg.data, marker.Data)
		if at < 0 {
			continue
		}
		wire := int(binary.LittleEndian.Uint32(msg.data[at+13 : at+17]))
		if wire != expiry {
			t.Fatalf("inventory packet expiry=%d, stored expiry=%d", wire, expiry)
		}
		return
	}
	t.Fatal("costume inventory packet missing")
}
