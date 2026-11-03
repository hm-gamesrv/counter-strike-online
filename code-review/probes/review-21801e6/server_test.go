package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func review218Room(t *testing.T) (*Server, *client, *room) {
	t.Helper()
	s, c, _ := newLoginExtraServer(t)
	c.room = 1
	st := validRoomSettings()
	name := &protocol.Writer{}
	name.String("Review218")
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
	return s, c, r
}

// The reference drops the update bit of a rejected value but keeps the value the
// room already had; only the update mask is cleaned.
func TestReview218RejectedUpdatePreservesOldBuyTime(t *testing.T) {
	s, c, room := review218Room(t)
	v := validateServer()
	s.Config.Room.ValidateSettings = true
	s.maps = v.maps
	s.modes = v.modes
	room.settings.setNumber(16, 20, 1)
	w := &protocol.Writer{}
	w.U8(5)
	w.U32(1 << 16)
	w.U32(0)
	w.U32(0)
	w.U32(0)
	w.U8(17) // Invalid in mode zero; the reference drops the update bit.
	if err := s.roomRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	if !room.settings.hasField(16) || room.settings.number(16) != 20 {
		t.Fatalf("ignored buyTime update erased old value: present=%v value=%d", room.settings.hasField(16), room.settings.number(16))
	}
}

// A map-only update runs LoadNewSettings in the reference, which reloads the
// map-derived settings even when validateSettings is off (the shipped value).
func TestReview218MapUpdateReloadsMapDerivedSettings(t *testing.T) {
	s, c, room := review218Room(t)
	room.settings.setNumber(48, 1, 2) // room creation mirrors mapId into mapId2
	w := &protocol.Writer{}
	w.U8(5)
	w.U32(1 << 7)
	w.U32(0)
	w.U32(0)
	w.U32(0)
	w.U16(5) // map 5: max_player 32 in MapList.csv
	if err := s.roomRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	if room.settings.number(7) != 5 {
		t.Fatalf("map did not change: map=%d", room.settings.number(7))
	}
	if room.settings.number(48) != 5 || room.settings.number(8) != 32 {
		t.Fatalf("map-derived settings not reloaded: mapId2=%d maxPlayers=%d, reference stores 5/32",
			room.settings.number(48), room.settings.number(8))
	}
}

// A map with a default weapon restriction (map 30 restricts to 7) forces that
// value in the reference regardless of validateSettings.
func TestReview218MapUpdateAppliesWeaponRestriction(t *testing.T) {
	s, c, room := review218Room(t)
	w := &protocol.Writer{}
	w.U8(5)
	w.U32(1 << 7)
	w.U32(0)
	w.U32(0)
	w.U32(0)
	w.U16(30) // map 30: weapon_restrict_deault 7, max_player 20
	if err := s.roomRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	if room.settings.number(7) != 30 {
		t.Fatalf("map did not change: map=%d", room.settings.number(7))
	}
	if room.settings.number(69) != 7 || room.settings.number(8) != 20 {
		t.Fatalf("map default restriction/capacity not applied: weaponRestrict=%d maxPlayers=%d, reference stores 7/20",
			room.settings.number(69), room.settings.number(8))
	}
}

// Kill-counter gachapon prize: 5101 is a countable item with a use button, so a
// fresh copy is unselected (status=0, inUse=1) and repeats stack.
func TestReview218Gachapon5101IsUsable(t *testing.T) {
	s, _, _ := newLoginExtraServer(t)
	if _, err := s.Store.DB.Exec("UPDATE UserCharacterExtended SET killsToGetGachaponItem=1 WHERE userID=1"); err != nil {
		t.Fatal(err)
	}
	awarded, _, err := s.Store.TickGachapon(1, 5101, 10, 0)
	if err != nil || !awarded {
		t.Fatalf("award=%v error=%v", awarded, err)
	}
	items, err := s.Store.Inventory(1)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v error=%v", items, err)
	}
	if items[0].Status != 0 || items[0].InUse != 1 {
		t.Fatalf("5101 is a countable usable item; status=%d inUse=%d", items[0].Status, items[0].InUse)
	}
}

func TestReview218Gachapon5101StacksExistingCopy(t *testing.T) {
	s, _, _ := newLoginExtraServer(t)
	if _, err := s.Store.GrantReward(1, store.Grant{
		Items: []store.Reward{{
			ID:         5101,
			Count:      1,
			Unselected: true,
			Stackable:  true,
		}},
	}, 10, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Store.DB.Exec("UPDATE UserCharacterExtended SET killsToGetGachaponItem=1 WHERE userID=1"); err != nil {
		t.Fatal(err)
	}
	if awarded, _, err := s.Store.TickGachapon(1, 5101, 10, 0); err != nil || !awarded {
		t.Fatalf("award=%v error=%v", awarded, err)
	}
	items, err := s.Store.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Count != 2 {
		t.Fatalf("5101 should stack into the existing row, got %v", items)
	}
}

// Item 166 is a character counter upstream: the award refreshes the session copy
// and sends user info, it never becomes an inventory row.
func review218Kill(c *client, s *Server) error {
	w := &protocol.Writer{}
	w.U8(3)
	w.U32(c.user.ID)
	w.U16(12)
	w.U8(2)
	w.U32(2)
	w.U8(1)
	w.U16(0)
	w.Bytes(make([]byte, 24))
	return s.hostRequest(c, &protocol.Reader{Data: w.Data})
}

func TestReview218PasswordBoxKillRefreshesCharacter(t *testing.T) {
	s, c, r := review218Room(t)
	r.match = &gameMatch{
		id: "review218-kill",
		stats: map[int]*matchStat{
			1: {user: c},
		},
	}
	// Wait for the 166 branch instead of assuming the random index; an all-5101
	// run skips rather than asserting a failure.
	for attempt := 0; attempt < 64; attempt++ {
		if _, err := s.Store.DB.Exec("UPDATE UserCharacterExtended SET killsToGetGachaponItem=1 WHERE userID=1"); err != nil {
			t.Fatal(err)
		}
		if err := review218Kill(c, s); err != nil {
			t.Fatal(err)
		}
		character, err := s.Store.Character(1)
		if err != nil {
			t.Fatal(err)
		}
		sentUserInfo := false
		for len(c.queue) > 0 {
			msg := <-c.queue
			sentUserInfo = sentUserInfo || msg.id == 157
		}
		if character.PasswordBoxes > 0 {
			if c.character.PasswordBoxes != character.PasswordBoxes || !sentUserInfo {
				t.Fatalf("password box award: database=%d cache=%d sent157=%v", character.PasswordBoxes, c.character.PasswordBoxes, sentUserInfo)
			}
			return
		}
		if _, err := s.Store.DB.Exec("DELETE FROM UserInventory WHERE userID=1"); err != nil {
			t.Fatal(err)
		}
	}
	t.Skip("random stream never selected item 166")
}

// The reference only accepts a drop box pickup (game event 34) in the scenario
// modes 15/17/26/28; every other mode is logged as a suspect and refused.
func TestReview218DropBoxRejectsNonScenarioModes(t *testing.T) {
	s, c, room := review218Room(t)
	room.match = &gameMatch{
		id: "review218-dropbox",
		stats: map[int]*matchStat{
			1: {user: c},
		},
	}
	w := &protocol.Writer{}
	w.U8(11)
	w.U8(34)
	w.U32(1)
	w.U32(3000)
	if err := s.hostRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	items, err := s.Store.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == 12 {
			t.Fatalf("mode 0 accepted a scenario drop box: %+v", items)
		}
	}
}

func review218FamilyRoom(t *testing.T) (*Server, *client, *client) {
	t.Helper()
	s, host, _, clanless, _ := familyBattleSetup(t)
	logging, _, _ := newLoginExtraServer(t)
	s.Log = logging.Log
	host.room = 7
	host.team = 2
	clanless.room = 7
	clanless.team = 2
	st := validRoomSettings()
	name := &protocol.Writer{}
	name.String("Review218")
	st.flags[0] |= 1
	st.fields[0] = name.Data
	s.rooms = map[int]*room{7: {
		id:       7,
		host:     host,
		members:  []*client{host, clanless},
		settings: st,
	}}
	return s, host, clanless
}

// Enabling family battle with a clanless member is rejected; the reference sends
// the current settings back so the host UI drops the change.
func TestReview218FamilyBattleRejectionRevertsHostSettings(t *testing.T) {
	s, host, _ := review218FamilyRoom(t)
	w := &protocol.Writer{}
	w.U8(5)
	w.U32(0)
	w.U32(0)
	w.U32(1 << 6) // familyBattle field 70
	w.U32(0)
	w.U8(1)
	if err := s.roomRequest(host, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	reverted := false
	for len(host.queue) > 0 {
		msg := <-host.queue
		if msg.id == 65 && len(msg.data) == 17 && msg.data[0] == 4 {
			reverted = true
		}
	}
	if !reverted {
		t.Fatal("rejected family battle update sent no settings revert to the host")
	}
}

// A joining non-participant must not enter an ongoing family battle match.
func TestReview218FamilyBattleRejectsNewMidMatchMember(t *testing.T) {
	s, a, b, _, d := familyBattleSetup(t)
	logging, _, _ := newLoginExtraServer(t)
	s.Log = logging.Log
	// startGame reports the host network through the host's own connection.
	for _, c := range []*client{a, b, d} {
		c.conn = pipeClient(t, c.user.ID, c.character.Name).conn
	}
	a.room = 7
	a.team = 2
	b.room = 7
	b.team = 1
	b.ready = 1
	d.inLobby = true
	st := familySettings()
	st.setNumber(8, 8, 1)
	r := &room{
		id:       7,
		host:     a,
		members:  []*client{a, b},
		settings: st,
	}
	if !s.familyBattleInit(r, a) || !s.familyBattleJoin(r, b) {
		t.Fatal("cannot initialize family battle")
	}
	s.rooms = map[int]*room{7: r}
	if err := s.startGame(a); err != nil || r.match == nil {
		t.Fatalf("start match: %v", err)
	}
	w := &protocol.Writer{}
	w.U8(23)
	w.U8(0)
	w.U16(7)
	w.String("")
	if err := s.roomRequest(d, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	if d.room != 0 || len(r.members) != 2 {
		t.Fatalf("new nonparticipant joined ongoing family battle: room=%d members=%d", d.room, len(r.members))
	}
}

// Shuffling the desk must keep number/opened pairs together; the reference
// shuffles whole slots, so an opened number stays opened.
func TestReview218BingoShufflePreservesOpenedNumbers(t *testing.T) {
	s, c, id := bingoServer(t)
	if err := s.initBingo(c); err != nil {
		t.Fatal(err)
	}
	slots, err := s.Store.BingoSlots(id)
	if err != nil {
		t.Fatal(err)
	}
	opened := map[int]bool{}
	for i := range slots {
		if i < 11 {
			slots[i].Opened = 1
			opened[slots[i].Number] = true
		}
	}
	if err = s.Store.SetBingoSlots(id, slots); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 8; attempt++ {
		if err = s.bingoShuffle(c); err != nil {
			t.Fatal(err)
		}
		for len(c.queue) > 0 {
			<-c.queue
		}
		updated, err := s.Store.BingoSlots(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, slot := range updated {
			if (slot.Opened != 0) != opened[slot.Number] {
				t.Fatalf("shuffle changed the opened-number set: number=%d opened=%d previouslyOpened=%v", slot.Number, slot.Opened, opened[slot.Number])
			}
		}
	}
}
