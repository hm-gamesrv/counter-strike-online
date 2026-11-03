package server

import (
	"net"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// TestOfflineChoiceRewardKeepsNotice: a reward that needs the selection dialog
// must stay queued and must not abort the login packet chain.
func TestOfflineChoiceRewardKeepsNotice(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	reward := s.rewards[3000]
	reward.Select = true
	s.rewards[3000] = reward
	if _, err := db.DB.Exec("INSERT INTO UserRewardNotice(userID,rewardID) VALUES(1,3000)"); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if err := s.grantRewardNotices(c); err != nil {
			t.Fatalf("login attempt %d aborted: %v", attempt, err)
		}
	}
	pending, err := db.RewardNotices(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0] != 3000 {
		t.Fatalf("pending notices = %v, want [3000]", pending)
	}
}

// TestExpiryNoticesDeleteOnlyDelivered: the queue is read and deleted by row
// identity, so a reversed insertion order keeps exactly the unsent rows.
func TestExpiryNoticesDeleteOnlyDelivered(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	if _, err := db.DB.Exec("WITH RECURSIVE n(x) AS (SELECT 260 UNION ALL SELECT x-1 FROM n WHERE x>1) INSERT INTO UserExpiryNotice(userID,itemID) SELECT 1,x FROM n"); err != nil {
		t.Fatal(err)
	}
	if err := s.sendExpiryNotices(c); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	reader := &protocol.Reader{Data: msg.data}
	if reader.U8() != 62 || reader.U8() != 255 {
		t.Fatal("unexpected notice header")
	}
	delivered := map[int]bool{}
	for range 255 {
		delivered[reader.U16()] = true
		reader.U8()
		reader.U8()
	}
	remaining, err := db.ExpiryNotices(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 5 {
		t.Fatalf("remaining = %v, want the 5 unsent rows", remaining)
	}
	for _, item := range remaining {
		if delivered[item] {
			t.Fatalf("deleted notice %d although it was never sent; remaining = %v", item, remaining)
		}
	}
}

// TestDedicatedServerReusedAfterRotation: a playlist rotation keeps the room's
// dedicated server, and the next map must start on it without a fresh pool slot.
func TestDedicatedServerReusedAfterRotation(t *testing.T) {
	s, r, _ := rewardMatch(t)
	s.Config.Room.HostConnectingMethod = 1
	r.settings = rotationSettings([]int{1, 4}, 1)
	r.settings.setNumber(6, 4, 1)
	r.settings.setNumber(8, 8, 1)
	d := &dedicatedServer{
		client: pipeClient(t, 0, "Dedicated"),
		ip:     net.IPv4(127, 0, 0, 1),
		port:   27015,
		room:   r.id,
	}
	d.client.room = r.id
	r.dedicated = d
	s.dedicated = []*dedicatedServer{d}
	if err := s.endGame(r, false); err != nil {
		t.Fatal(err)
	}
	if r.dedicated != d || d.room != r.id {
		t.Fatal("the retained dedicated server was released at the second playlist map")
	}
	if err := s.startGame(r.host); err != nil {
		t.Fatal(err)
	}
	if r.match == nil {
		t.Fatal("the second map did not reuse the retained dedicated server")
	}
}

// TestTimedCosmeticStaysVisibleAfterToggle: timed nameplates and effect items
// are dormant until used, and activating one must convert the relative duration
// instead of making the item look expired.
func TestTimedCosmeticStaysVisibleAfterToggle(t *testing.T) {
	for _, class := range []string{"LobbyBG", "zbRespawnEffect", "CombatInfoItem"} {
		t.Run(class, func(t *testing.T) {
			s, c, db := newLoginExtraServer(t)
			s.items[7001] = itemDefinition{ClassName: class}
			grants, err := s.prepareGrants([]store.Reward{{
				ID:       7001,
				Count:    1,
				Duration: 3,
			}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.GrantReward(1, store.Grant{Items: grants}, 100, 0); err != nil {
				t.Fatal(err)
			}
			w := &protocol.Writer{}
			w.U8(0)
			w.U8(0)
			w.U16(2)
			w.U8(0)
			w.U16(1)
			if err = s.itemRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
				t.Fatal(err)
			}
			items, err := db.Inventory(1)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range items {
				if item.ID == 7001 {
					return
				}
			}
			var status, inUse, expiry int
			if err = db.DB.QueryRow("SELECT status,inUse,expiryDate FROM UserInventory WHERE userID=1 AND itemID=7001").Scan(&status, &inUse, &expiry); err != nil {
				t.Fatal(err)
			}
			t.Fatalf("timed cosmetic disappeared: status=%d inUse=%d expiry=%d", status, inUse, expiry)
		})
	}
}

// TestDuplicateCostumeKeepsOneEquipped: two copies of the same costume must never
// stay equipped together, whichever copy is activated.
func TestDuplicateCostumeKeepsOneEquipped(t *testing.T) {
	for _, class := range []string{"HeadCostume", "ZombieSkinCostume"} {
		t.Run(class, func(t *testing.T) {
			_, _, db := newLoginExtraServer(t)
			_, err := db.GrantReward(1, store.Grant{Items: []store.Reward{
				{
					ID:       471,
					Count:    1,
					Duration: 3,
					Dormant:  true,
				},
				{
					ID:       471,
					Count:    1,
					Duration: 3,
					Dormant:  true,
				},
			}}, 100, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, slot := range []int{2, 3} {
				if _, err = db.ToggleCostume(1, slot, class, 0); err != nil {
					t.Fatal(err)
				}
			}
			var equipped int
			if err = db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=471 AND inUse=1").Scan(&equipped); err != nil {
				t.Fatal(err)
			}
			if equipped != 1 {
				t.Fatalf("%s has %d equipped copies, want 1", class, equipped)
			}
		})
	}
}

// TestUnequippedCostumeExtensionAddsDays: an activated-then-unequipped costume
// keeps an absolute expiry, so buying two extenders must add two days.
func TestUnequippedCostumeExtensionAddsDays(t *testing.T) {
	_, _, db := newLoginExtraServer(t)
	if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{
		{
			ID:       471,
			Count:    1,
			Duration: 3,
			Dormant:  true,
		},
		{
			ID:         676,
			Count:      10,
			Unselected: true,
		},
	}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := db.ToggleCostume(1, 2, "HeadCostume", 0); err != nil {
			t.Fatal(err)
		}
	}
	var before, after int
	if err := db.DB.QueryRow("SELECT expiryDate FROM UserInventory WHERE userID=1 AND slot=2").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if before < int(time.Now().Unix()/60) {
		t.Fatal("fixture expiry is not absolute")
	}
	if _, err := db.ExtendItem(1, 2, 676, 2); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.QueryRow("SELECT expiryDate FROM UserInventory WHERE userID=1 AND slot=2").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after-before != 2880 {
		t.Fatalf("two extenders added %d minutes, want 2880", after-before)
	}
}

// TestProtectedItemsUseItemIDs: the protected list holds item IDs, so a default
// item in another slot must still survive disassembly and consumption.
func TestProtectedItemsUseItemIDs(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	s.Config.DefaultUser.PseudoDefaultItems = []int{12}
	if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{{
		ID:    12,
		Count: 1,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	request := []byte{7, 1, 0, 0, 2, 0, 0, 0, 0, 0}
	if err := s.itemRequest(c, &protocol.Reader{Data: request}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=12").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("disabled item 12 was removed by a disassemble request")
	}
	if _, err := db.ConsumeItem(1, 12, []int{12}); err == nil {
		t.Fatal("ConsumeItem ignored the protected item ID")
	}
}

// TestFailedAddonRequestKeepsItems: parsing errors, tail bytes and a failed save
// must all leave the materials in the bag.
func TestFailedAddonRequestKeepsItems(t *testing.T) {
	for _, failure := range []string{"truncated", "tail", "database"} {
		t.Run(failure, func(t *testing.T) {
			s, c, db := newLoginExtraServer(t)
			c.room = 1
			s.rooms = map[int]*room{1: {id: 1}}
			s.items[7001] = itemDefinition{ClassName: "zbsaddonitem"}
			if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{{
				ID:         7001,
				Count:      1,
				Unselected: true,
			}}}, 100, 0); err != nil {
				t.Fatal(err)
			}
			w := &protocol.Writer{}
			if failure == "truncated" {
				w.U16(2)
			} else {
				w.U16(1)
			}
			w.U16(7001)
			if failure == "tail" {
				w.U8(99)
			}
			if failure == "database" {
				if _, err := db.DB.Exec("CREATE TRIGGER failAddon BEFORE INSERT ON UserAddon BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.setZBAddons(c, &protocol.Reader{Data: w.Data}); err == nil {
				t.Fatal("expected the request to fail")
			}
			var count int
			if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=7001").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatal("failed addon request consumed the item")
			}
		})
	}
}

// TestRepeatedAddonIDConsumesOneUnit: a deleted stack must not be offered to the
// next addon entry of the same request.
func TestRepeatedAddonIDConsumesOneUnit(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	c.room = 1
	s.rooms = map[int]*room{1: {id: 1}}
	s.items[7001] = itemDefinition{ClassName: "zbsaddonitem"}
	if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{{
		ID:         7001,
		Count:      1,
		Unselected: true,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	w := &protocol.Writer{}
	w.U16(6)
	for range 6 {
		w.U16(7001)
	}
	if err := s.setZBAddons(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	addons, err := db.Addons(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(addons) != 1 {
		t.Fatalf("one owned unit paid for %d addon entries: %v", len(addons), addons)
	}
}
