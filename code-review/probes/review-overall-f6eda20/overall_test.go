package server

import (
	"net"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestOverallDedicatedPlaylistCanStartNextMap(t *testing.T) {
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
	if r.dedicated != d || d.room != r.id || r.settings.number(42) != 2 {
		t.Fatal("fixture did not retain the dedicated server at the second playlist map")
	}
	if err := s.startGame(r.host); err != nil {
		t.Fatal(err)
	}
	if r.match == nil {
		t.Fatal("second map cannot start despite owning a retained dedicated server")
	}
}

func TestOverallDuplicateCostumeHasOneEquippedCopy(t *testing.T) {
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
				t.Fatalf("same costume has %d equipped copies, want 1", equipped)
			}
		})
	}
}

func TestOverallTimedCosmeticsRemainVisibleAfterToggle(t *testing.T) {
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

func TestOverallUnequippedCostumeExtensionUsesMinutes(t *testing.T) {
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
		t.Fatalf("two extenders bought %d minutes, want 2880", after-before)
	}
}

func TestOverallDisassemblePreservesPseudoDefaultID(t *testing.T) {
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
		t.Fatal("protected pseudo-default item 12 was deleted from slot 2")
	}
}

func TestOverallConsumePreservesProtectedItemID(t *testing.T) {
	_, _, db := newLoginExtraServer(t)
	if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{{
		ID:    12,
		Count: 1,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ConsumeItem(1, 12, []int{12}); err == nil {
		t.Fatal("protected item ID 12 was consumed from slot 2")
	}
}

func TestOverallTruncatedAddonsDoNotConsumeItems(t *testing.T) {
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
				t.Fatal("expected request failure")
			}
			var count int
			if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=7001").Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatal("failed addon request permanently consumed the first item")
			}
		})
	}
}
