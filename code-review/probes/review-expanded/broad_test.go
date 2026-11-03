package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestAuditOfflineRewardAtomicity(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	if _, err := db.DB.Exec("INSERT INTO UserRewardNotice(userID,rewardID) VALUES(1,3000)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("CREATE TRIGGER failNotice BEFORE DELETE ON UserRewardNotice BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.grantRewardNotices(c); err == nil {
		t.Fatal("expected notice deletion to fail")
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
		t.Fatalf("offline reward issued %d times after deletion failure and retry, want 1", count)
	}
}

func TestAuditBingoAwardAtomicity(t *testing.T) {
	s, c, id := bingoServer(t)
	if err := s.initBingo(c); err != nil {
		t.Fatal(err)
	}
	slots, err := s.Store.BingoSlots(id)
	if err != nil {
		t.Fatal(err)
	}
	for i := range slots {
		slots[i].Opened = 1
	}
	slots[0].Opened = 0
	if err = s.Store.SetBingoSlots(id, slots); err != nil {
		t.Fatal(err)
	}
	// All earlier lines are already claimed. Cell 0 now completes row 0,
	// column 0 and the main diagonal, so precisely three prizes remain.
	if _, err = s.Store.DB.Exec("UPDATE UserMiniGameBingoPrizeSlot SET opened=CASE WHEN idx IN (0,5,10) THEN 0 ELSE 1 END WHERE userID=?", id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.DB.Exec("CREATE TRIGGER failBingo BEFORE UPDATE ON UserMiniGameBingoSlot BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.bingoOpen(c); err == nil {
		t.Fatal("expected bingo state update failure")
	}
	if _, err = s.Store.DB.Exec("DROP TRIGGER failBingo"); err != nil {
		t.Fatal(err)
	}
	if err = s.bingoOpen(c); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.Store.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=? AND itemID=567", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("bingo issued %d rewards for 3 remaining prizes after retry, want 3", count)
	}
}

func TestAuditWeaponReleaseKeepsEarnedPrize(t *testing.T) {
	s, c, id := weaponReleaseServer(t, true)
	s.Config.InventorySlotMax = 1
	if _, err := s.Store.DB.Exec("INSERT INTO UserMiniGameWeaponReleaseItemProgress(userID,slot,character,opened) VALUES(?,567,30,0)", id); err != nil {
		t.Fatal(err)
	}
	// The final G fills bit 0 of GALIL. The starter gift fills the bag.
	request := []byte{0, 4, 'G'}
	if err := s.weaponReleaseSet(c, &protocol.Reader{Data: request}); err != nil {
		t.Fatal(err)
	}
	s.Config.InventorySlotMax = 100
	if err := s.weaponReleaseSet(c, &protocol.Reader{Data: request}); err != nil {
		t.Fatal(err)
	}
	var count, opened int
	if err := s.Store.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=? AND itemID=567", id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := s.Store.DB.QueryRow("SELECT opened FROM UserMiniGameWeaponReleaseItemProgress WHERE userID=? AND slot=567", id).Scan(&opened); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("completed word opened=%d, awarded=%d even after space was freed; want 1 prize", opened, count)
	}
}

func TestAuditEnhanceAmmoIsInDraw(t *testing.T) {
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
		t.Fatalf("Ammo never drawn from six eligible attributes: %v", counts)
	}
}

func TestAuditDecoderUsesSelectionRules(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	if _, err := db.GrantReward(1, store.Grant{
		Items: []store.Reward{
			{
				ID:         8292,
				Count:      2,
				Unselected: true,
			},
		},
	}, 100, 0); err != nil {
		t.Fatal(err)
	}
	s.items[8292] = itemDefinition{ClassName: "LotteryKeyItem"}
	s.itemBoxes = map[int]*itemBox{
		8292: {
			ID:    8292,
			Total: 1,
			Rates: []itemBoxRate{
				{
					Rate:      1,
					Durations: []int{0},
					Items:     []int{12},
				},
			},
		},
	}
	if err := s.openItemBox(c, 2, 2); err != nil {
		t.Fatal(err)
	}
	var active int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=12 AND inUse=1 AND status=1").Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active > 1 {
		t.Fatalf("decoder selected %d copies of one weapon, want at most 1", active)
	}
}

func TestAuditExpiryNoticeRemainder(t *testing.T) {
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
		t.Fatalf("255 of 260 notices sent, but remaining=%d, want 5", remaining)
	}
}

func TestAuditBingoPartialInventoryRepeatsPrize(t *testing.T) {
	s, c, id := bingoServer(t)
	s.Config.InventorySlotMax = 2
	if err := s.initBingo(c); err != nil {
		t.Fatal(err)
	}
	slots, err := s.Store.BingoSlots(id)
	if err != nil {
		t.Fatal(err)
	}
	for i := range slots {
		slots[i].Opened = 1
	}
	slots[0].Opened = 0
	if err = s.Store.SetBingoSlots(id, slots); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.DB.Exec("UPDATE UserMiniGameBingoPrizeSlot SET opened=CASE WHEN idx IN (0,5,10) THEN 0 ELSE 1 END WHERE userID=?", id); err != nil {
		t.Fatal(err)
	}
	if err = s.bingoOpen(c); err != nil {
		t.Fatal(err)
	}
	// Free one inventory space, just as disassembly would, then retry.
	if _, err = s.Store.DB.Exec("DELETE FROM UserInventory WHERE userID=? AND itemID=1001", id); err != nil {
		t.Fatal(err)
	}
	if err = s.bingoOpen(c); err != nil {
		t.Fatal(err)
	}
	var awarded, marked int
	if err = s.Store.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=? AND itemID=567", id).Scan(&awarded); err != nil {
		t.Fatal(err)
	}
	if err = s.Store.DB.QueryRow("SELECT count(*) FROM UserMiniGameBingoPrizeSlot WHERE userID=? AND idx IN (0,5,10) AND opened=1", id).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	if awarded > 1 && marked == 0 {
		t.Fatalf("normal bag-full retry already issued %d prizes but marked %d claimed", awarded, marked)
	}
}
