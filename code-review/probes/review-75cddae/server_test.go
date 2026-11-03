package server

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// reviewHeadQuestServer wires a quest-capable server around the login fixture.
// The caller installs s.quests and then feeds kill events through applyQuestEvent.
func reviewHeadQuestServer(t *testing.T) (*Server, *client, *gameMatch, *matchStat) {
	t.Helper()
	s, c, _ := newLoginExtraServer(t)
	c.playing = true
	stat := &matchStat{
		user: c,
	}
	m := &gameMatch{
		id:    "review-head",
		mode:  0,
		mapID: 1,
		stats: map[int]*matchStat{1: stat},
	}
	return s, c, m, stat
}

func reviewHeadKill() questEvent {
	return questEvent{
		Kind:       2,
		GunID:      242,
		KillerTeam: 2,
		VictimTeam: 1,
		VictimID:   2,
	}
}

// CQuestEventConditionKill::OnKillEvent counts the streak first and only pays on
// the kill that finds the counter already at Continuous, so a "2 continuous
// kills" condition pays its first point on the third kill
// (questevent.h:241-251). The probe sends four eligible kills for a two-unit
// goal; the reference has not finished the task yet.
func TestReviewHeadContinuousKillWindow(t *testing.T) {
	s, c, m, stat := reviewHeadQuestServer(t)
	s.quests = []questTask{{
		QuestID: 1,
		TaskID:  1,
		Goal:    2,
		Conditions: []questCondition{{
			EventType:      2,
			GoalPoints:     1,
			Continuous:     2,
			KillerTeam:     -1,
			VictimTeam:     -1,
			VictimKillType: -1,
			GunID:          -1,
			Bot:            -1,
		}},
	}}
	for i := 0; i < 4; i++ {
		if err := s.applyQuestEvent(c, m, stat, reviewHeadKill()); err != nil {
			t.Fatal(err)
		}
	}
	progress, err := s.Store.QuestProgress(1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Units >= 2 {
		t.Fatalf("continuous=2 paid out after 4 kills (units=%d); the reference needs 3 kills for the first point and 3 more for the second", progress.Units)
	}
}

// CQuestEventBaseConditionGameMatch::IncrementTempCount notifies on every exact
// multiple of Notice.Goal (questevent.h:141-147). A 15-point condition therefore
// notifies on 30, 60, ... but not on 15, while the probe sees what Go does after
// two 15-point events.
func TestReviewHeadMilestoneNoticeCondition(t *testing.T) {
	s, c, m, stat := reviewHeadQuestServer(t)
	s.quests = []questTask{{
		QuestID: 2,
		TaskID:  1,
		Goal:    100,
		Conditions: []questCondition{{
			EventType:      2,
			GoalPoints:     15,
			KillerTeam:     -1,
			VictimTeam:     -1,
			VictimKillType: -1,
			GunID:          -1,
			Bot:            -1,
		}},
	}}
	s.quests[0].Notice.Goal = 10
	s.quests[0].Notice.UserMsg = "%d/%d"
	for i := 0; i < 2; i++ {
		if err := s.applyQuestEvent(c, m, stat, reviewHeadKill()); err != nil {
			t.Fatal(err)
		}
	}
	notices := 0
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id == 67 {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("two 15-point kills produced %d chat notices; the reference notifies only on exact multiples of Notice.Goal (30, 60, ...)", notices)
	}
}

// CPacketManager::SendShopBuyProductReply always writes U8(1) as the second byte,
// whatever the result (packetmanager.cpp:2108-2130).
func TestReviewHeadShopFailureReplyByte(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "shop.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("buyer", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Buyer", config.DefaultUser{Level: 1, Points: 5}); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store: db,
		Config: config.Config{
			InventorySlotMax: 100,
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	c := &client{
		user: store.User{
			ID: 1,
		},
		queue: make(chan outbound, 8),
	}
	if err = s.shopRequest(c, &protocol.Reader{Data: []byte{1, 200, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	var reply []byte
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id == 72 {
			reply = msg.data
		}
	}
	if len(reply) < 2 || reply[0] != 1 {
		t.Fatalf("unexpected purchase reply %v", reply)
	}
	if reply[1] != 1 {
		t.Fatalf("failed purchase reply writes %d as its second byte; the reference always writes 1", reply[1])
	}
}

// The reference's AddItem sends SendInventoryAdd for the row it added or
// extended (itemmanager.cpp:771-773), not the whole bag; the purchase reply
// follows the point update. A purchase must not resend 152.
func TestReviewHeadShopPurchaseInventoryPackets(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "shop2.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("shopper", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Shopper", config.DefaultUser{Level: 1, Points: 5}); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:  db,
		Config: config.Config{InventorySlotMax: 100},
		Log:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		items:  map[int]itemDefinition{212: {Category: 6}},
		shop: shop{Products: []product{{
			Item:   212,
			Points: true,
			Subs: []subproduct{{
				ID:    56,
				Price: 1,
				Items: []store.Reward{{
					ID:       212,
					Count:    1,
					Duration: 3,
				}},
			}},
		}}},
	}
	c := &client{
		user: store.User{
			ID: 1,
		},
		queue: make(chan outbound, 16),
	}
	if err = s.shopRequest(c, &protocol.Reader{Data: []byte{1, 56, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	ids := map[byte]int{}
	for len(c.queue) > 0 {
		msg := <-c.queue
		ids[msg.id]++
	}
	if ids[152] != 0 {
		t.Fatalf("purchase resent the whole inventory (152 x%d, 154 x%d); the reference sends one incremental 154", ids[152], ids[154])
	}
}

// The map table owns capacity, the default weapon restriction, the ball count
// and mapId2 on every map change (roomsettings.cpp:2169-2192, 2313-2320). The
// fixture needs s.maps populated, which review-21801e6's room fixture lacked.
func TestReviewHeadMapDefaultsWithMapTable(t *testing.T) {
	s, c, _ := newLoginExtraServer(t)
	c.room = 1
	st := validRoomSettings()
	name := &protocol.Writer{}
	name.String("ReviewHead")
	st.flags[0] |= 1
	st.fields[0] = name.Data
	r := &room{
		id:       1,
		host:     c,
		members:  []*client{c},
		settings: st,
	}
	s.rooms = map[int]*room{1: r}
	s.users = map[int]*client{1: c}
	s.maps = map[int]mapRow{
		5: {
			MaxPlayer: 32,
			Values:    map[string]int{"map_id": 5},
		},
		30: {
			MaxPlayer: 20,
			Values:    map[string]int{"map_id": 30, "weapon_restrict_deault": 7},
		},
	}
	change := func(mapID int) {
		t.Helper()
		w := &protocol.Writer{}
		w.U8(5)
		w.U32(1 << 7)
		w.U32(0)
		w.U32(0)
		w.U32(0)
		w.U16(mapID)
		if err := s.roomRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
			t.Fatal(err)
		}
	}
	change(5)
	if r.settings.number(48) != 5 || r.settings.number(8) != 32 {
		t.Fatalf("map-derived settings not reloaded: mapId2=%d maxPlayers=%d", r.settings.number(48), r.settings.number(8))
	}
	// The fields the map block wrote must join the broadcast (settings.writeUpdate's
	// added mask), otherwise the client never repaints capacity/ball/playlist.
	var mask [4]uint32
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id == 65 && len(msg.data) >= 17 && msg.data[0] == 4 {
			reader := &protocol.Reader{Data: msg.data[1:]}
			for i := range mask {
				mask[i] = uint32(reader.U32())
			}
		}
	}
	for _, field := range []int{8, 38, 48, 59} {
		if mask[field/32]&(1<<uint(field%32)) == 0 {
			t.Fatalf("map update broadcast mask %x is missing field %d", mask, field)
		}
	}
	change(30)
	if r.settings.number(69) != 7 || r.settings.number(8) != 20 || r.settings.number(48) != 30 {
		t.Fatalf("map defaults: restriction=%d maxPlayers=%d mapId2=%d", r.settings.number(69), r.settings.number(8), r.settings.number(48))
	}
}

// The two gachapon probes of review-21801e6 asserted len(items)==1 while the
// login fixture already holds the default item 1001. Filtering by item ID shows
// the product behaviour the response claims: a fresh 5101 is unselected and a
// repeat award merges into that row.
func TestReviewHeadGachapon5101Shape(t *testing.T) {
	s, _, _ := newLoginExtraServer(t)
	if _, err := s.Store.DB.Exec("UPDATE UserCharacterExtended SET killsToGetGachaponItem=1 WHERE userID=1"); err != nil {
		t.Fatal(err)
	}
	awarded, award, err := s.Store.TickGachapon(1, 5101, 10, 0)
	if err != nil || !awarded {
		t.Fatalf("award=%v error=%v", awarded, err)
	}
	if len(award.Items) != 1 || award.Items[0].ID != 5101 || award.Items[0].Status != 0 || award.Items[0].InUse != 1 {
		t.Fatalf("fresh 5101=%+v", award.Items)
	}
	if _, err = s.Store.DB.Exec("UPDATE UserCharacterExtended SET killsToGetGachaponItem=1 WHERE userID=1"); err != nil {
		t.Fatal(err)
	}
	if awarded, award, err = s.Store.TickGachapon(1, 5101, 10, 0); err != nil || !awarded {
		t.Fatalf("award=%v error=%v", awarded, err)
	}
	items, err := s.Store.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	copies := make([]store.Item, 0, 2)
	for _, item := range items {
		if item.ID == 5101 {
			copies = append(copies, item)
		}
	}
	if len(copies) != 1 || copies[0].Count != 2 || copies[0].Status != 0 {
		t.Fatalf("repeat award did not merge into the existing row: %+v", copies)
	}
}
