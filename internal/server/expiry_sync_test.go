package server

import (
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// readInventoryPatch decodes one incremental inventory frame. The frame is
// slot-keyed: present=1 updates a slot, present=0 deletes it. Absent slots keep
// whatever the client already has, which is why a removal needs its own entry.
func readInventoryPatch(t *testing.T, data []byte) map[int]bool {
	t.Helper()
	r := &protocol.Reader{Data: data}
	r.U32()
	count := r.U16()
	seen := map[int]bool{}
	for range count {
		slot, present := r.U16(), r.U8()
		seen[slot] = present != 0
		if present == 0 {
			continue
		}
		r.U16() // item ID
		r.U16() // count
		r.U8()  // status
		r.U8()  // in use
		r.U32() // obtain date
		r.U32() // expiry date
		r.U16() // paint
		for range r.U16() {
			r.U16()
		}
		r.U16() // enhancement level
		r.U32() // enhancement experience
		r.U32() // enhancement value
		r.U8()  // disassemble progress
		r.U32()
		for range r.U8() {
			r.U8()
			r.U16()
		}
		r.U8() // in use
		r.U8()
		r.U8() // lock status
		r.U32()
		r.U8()
	}
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Pos != len(r.Data) {
		t.Fatalf("inventory patch has %d trailing bytes", len(r.Data)-r.Pos)
	}
	return seen
}

func insertInventoryRow(t *testing.T, db *store.Store, user, slot, item, expiry int) {
	t.Helper()
	_, err := db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,?,?,1,1,0,?,?,0,0,0,0,0,0,0,0)", user, slot, item, expiry-10, expiry)
	if err != nil {
		t.Fatal(err)
	}
}

// TestExpiryPushClearsDeletedSlot: the update frame cannot delete a slot by
// omission, so the sweep must send a present=0 record for every expired slot and
// keep sending the surviving rows.
func TestExpiryPushClearsDeletedSlot(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	c.queue = make(chan outbound, 256)
	s.users = map[int]*client{1: c}
	now := int(time.Now().Unix() / 60)
	insertInventoryRow(t, db, 1, 7, 12, now-1)
	insertInventoryRow(t, db, 1, 8, 12, 0)
	s.expireItems(time.Now())
	patches := 0
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id != 154 {
			continue
		}
		patches++
		seen := readInventoryPatch(t, msg.data)
		present, ok := seen[7]
		if !ok || present {
			t.Fatalf("expired slot 7 not deleted by the pushed patch: %v", seen)
		}
		if !seen[8] {
			t.Fatalf("surviving slot 8 missing from the pushed patch: %v", seen)
		}
	}
	if patches == 0 {
		t.Fatal("expiry pushed no inventory patch")
	}
}

// TestLoginSweepKeepsInventoryAfterUserStart: the sweep runs during login, but the
// account that is logging in initializes its bag later in the login chain. Pushing
// a bag update from the sweep sends 152/154 before UserStart 150; other online
// accounts must still receive their removal patch.
func TestLoginSweepKeepsInventoryAfterUserStart(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	c.queue = make(chan outbound, 256)
	if err := db.Register("otheruser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCharacter(2, "OtherUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	other := &client{
		user: store.User{
			ID: 2,
		},
		character: store.Character{
			ID:   2,
			Name: "OtherUser",
		},
		queue: make(chan outbound, 256),
	}
	s.users = map[int]*client{1: c, 2: other}
	now := int(time.Now().Unix() / 60)
	insertInventoryRow(t, db, 1, 7, 12, now-1)
	insertInventoryRow(t, db, 2, 7, 12, now-1)
	insertInventoryRow(t, db, 2, 8, 12, 0)
	if err := s.loginPackets(c); err != nil {
		t.Fatal(err)
	}
	started := false
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id == 150 {
			started = true
		}
		if (msg.id == 152 || msg.id == 154) && !started {
			t.Fatalf("inventory packet %d was queued before UserStart 150", msg.id)
		}
	}
	var rows int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND slot=7").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("login sweep left the expired row %d", rows)
	}
	patched := false
	for len(other.queue) > 0 {
		msg := <-other.queue
		if msg.id != 154 {
			continue
		}
		seen := readInventoryPatch(t, msg.data)
		if present, ok := seen[7]; ok && !present && seen[8] {
			patched = true
		}
	}
	if !patched {
		t.Fatal("online account did not receive the expiry patch")
	}
}
