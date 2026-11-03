package server

import (
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func review9dExpiredServer(t *testing.T) (*Server, *client) {
	t.Helper()
	s, c, db := newLoginExtraServer(t)
	c.queue = make(chan outbound, 256)
	s.users = map[int]*client{1: c}
	if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{{
		ID:       12,
		Count:    1,
		Duration: 1,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=1 AND slot=2", int(time.Now().Unix()/60)-1); err != nil {
		t.Fatal(err)
	}
	return s, c
}

func TestReview9dExpirySendsRemovedSlot(t *testing.T) {
	s, c := review9dExpiredServer(t)
	s.expireItems(time.Now())
	remaining, err := s.Store.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	bySlot := map[int]store.Item{}
	for _, item := range remaining {
		bySlot[item.Slot] = item
	}
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id != 154 {
			continue
		}
		r := &protocol.Reader{Data: msg.data}
		r.U32()
		count := r.U16()
		for range count {
			slot, present := r.U16(), r.U8()
			if present == 0 {
				if slot == 2 {
					return
				}
				continue
			}
			item, ok := bySlot[slot]
			if !ok {
				t.Fatalf("unexpected inventory slot %d", slot)
			}
			encoded := &protocol.Writer{}
			writeItem(encoded, item, slot, 0)
			r.Bytes(len(encoded.Data) - 3)
		}
		if r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	t.Fatal("expiry sent no slot=2,present=0 removal entry")
}

func TestReview9dLoginInventoryFollowsUserStart(t *testing.T) {
	s, c := review9dExpiredServer(t)
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
}
