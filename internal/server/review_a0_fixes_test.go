package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// TestLoginSweepReloadsExpiredSelection: skipping the bag push for the account
// that is still logging in must not skip the cache reload, otherwise the login
// chain advertises a nameplate the sweep just cleared.
func TestLoginSweepReloadsExpiredSelection(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	c.queue = make(chan outbound, 256)
	s.users = map[int]*client{1: c}
	if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{{
		ID:       7001,
		Count:    1,
		Duration: 1,
		Dormant:  true,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	if err := s.toggleNameplate(c, store.Item{
		ID:   7001,
		Slot: 2,
	}); err != nil {
		t.Fatal(err)
	}
	for len(c.queue) > 0 {
		<-c.queue
	}
	if _, err := db.DB.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=1 AND slot=2", int(time.Now().Unix()/60)-1); err != nil {
		t.Fatal(err)
	}
	if err := s.loginPackets(c); err != nil {
		t.Fatal(err)
	}
	fresh, err := db.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if c.character.NameplateID != fresh.NameplateID {
		t.Fatalf("login advertised expired background: cache=%d database=%d", c.character.NameplateID, fresh.NameplateID)
	}
}

// TestEnhanceCosmeticMaterialClearsSelection: the server does not restrict
// enhancement materials to weapons, so a material can be the equipped nameplate;
// consuming it has to clear that selection, as the reference does through
// OnItemUse/RemoveItem.
func TestEnhanceCosmeticMaterialClearsSelection(t *testing.T) {
	s, c, db := newEnhanceTestServer(t)
	d := s.items[9001]
	d.ClassName = "LobbyBG"
	s.items[9001] = d
	if _, err := db.DB.Exec("UPDATE UserInventory SET count=1 WHERE userID=1 AND slot=3"); err != nil {
		t.Fatal(err)
	}
	if err := s.toggleNameplate(c, store.Item{
		ID:   9001,
		Slot: 3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.itemRequest(c, &protocol.Reader{Data: []byte{14, 0, 0, 2, 0, 1, 0, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=9001").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("material was not consumed: %d rows", rows)
	}
	fresh, err := db.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.NameplateID != 0 {
		t.Fatalf("consumed cosmetic material left reference=%d", fresh.NameplateID)
	}
}

// TestEnhanceMaterialDoublesRunningTotal pins the reference accumulation order:
// a same-ID material doubles everything collected so far, not just itself.
func TestEnhanceMaterialDoublesRunningTotal(t *testing.T) {
	materials := []store.Item{
		{
			ID:    200,
			Count: 1,
		},
		{
			ID:    100,
			Count: 1,
		},
	}
	grade := func(item store.Item) int {
		if item.ID == 200 {
			return 6 // 500 table experience
		}
		return 1 // 10 table experience
	}
	// (500 + 10) * 2 = 1020, not 500 + 10*2 = 520.
	if got := enhanceMaterialExp(100, materials, grade); got != 1020 {
		t.Fatalf("material experience=%d, want (500+10)*2=1020", got)
	}
}

// TestExpiredClassItemLosesBonus: the class bonus is resolved at settlement with
// the reference's GetFirstActiveItemByItemID, so a class item removed by the
// expiry sweep must no longer pay the percentage.
func TestExpiredClassItemLosesBonus(t *testing.T) {
	s, room, stat := rewardMatch(t)
	if _, err := s.Store.DB.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=1 AND itemID=10", int(time.Now().Unix()/60)-1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.ExpireItems(int(time.Now().Unix() / 60)); err != nil {
		t.Fatal(err)
	}
	if err := s.endGame(room, true); err != nil {
		t.Fatal(err)
	}
	if stat.classExp != 0 || stat.bonusExp != 0 {
		t.Fatalf("deleted class still earned classPercent=%d bonusExp=%d", stat.classExp, stat.bonusExp)
	}
}

// TestVoxelReservedBitsCarryNoPayload: Voxel bits 18 and 20 have no flag
// definition and no read in the reference, so they must not consume the field
// that follows the Voxel block.
func TestVoxelReservedBitsCarryNoPayload(t *testing.T) {
	for _, bit := range []int{18, 20} {
		t.Run(fmt.Sprint(bit), func(t *testing.T) {
			w := &protocol.Writer{}
			w.U32(0)
			w.U32(1 << 18)
			w.U32(1)
			w.U32(0)
			w.U32(1 << bit)
			w.U8(77)
			r := &protocol.Reader{Data: w.Data}
			st, err := parseSettings(r)
			if err != nil || st.truncated || st.number(64) != 77 {
				t.Fatalf("reserved voxel bit %d consumed the following field: truncated=%v value=%d err=%v", bit, st.truncated, st.number(64), err)
			}
		})
	}
}
