package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestCapturedCurrentClientShopPurchase(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "shop.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("buyer", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Buyer", config.DefaultUser{Level: 1, Points: 5}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{InventorySlotMax: 100}, items: map[int]itemDefinition{212: {Category: 6}}, shop: shop{Products: []product{{Item: 212, Points: true, Subs: []subproduct{{ID: 56, Price: 1, Items: []store.Reward{{ID: 212, Count: 1, Duration: 3}}}}}}}}
	c := &client{user: store.User{ID: 1}, queue: make(chan outbound, 64)}
	reply := func() (int, string) {
		for len(c.queue) > 0 {
			msg := <-c.queue
			if msg.id == 72 {
				p := &protocol.Reader{Data: msg.data}
				if p.U8() != 1 {
					t.Fatal("wrong purchase reply")
				}
				return p.U8(), p.CString()
			}
		}
		t.Fatal("missing purchase reply")
		return 0, ""
	}
	for attempt := 0; attempt < 2; attempt++ {
		data := []byte{1, 56, 0, 0, 0}
		if err = s.shopRequest(c, &protocol.Reader{Data: data}); err != nil {
			t.Fatal(err)
		}
		if success, message := reply(); success != 1 || message != "BUY_OK" {
			t.Fatal(success, message)
		}
	}
	ch, _ := db.Character(1)
	items, _ := db.Inventory(1)
	now := int(time.Now().Unix() / 60)
	// Buying the same timed item twice extends the owned copy (AddItem runs
	// ExtendItem and returns) instead of opening a second row, so the duration
	// doubles: 3 days + 3 days.
	if ch.Points != 3 || len(items) != 2 || items[1].ID != 212 || items[1].Status != 1 || items[1].InUse != 1 {
		t.Fatal("purchase did not debit and grant timed weapon", ch, items)
	}
	if items[1].Expiry < now+8640-2 || items[1].Expiry > now+8640+2 {
		t.Fatalf("second purchase did not extend the owned copy: expiry-%d=%d, want ~8640", now, items[1].Expiry-now)
	}
	if err = s.shopRequest(c, &protocol.Reader{Data: []byte{1, 255, 255, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	// SendShopBuyProductReply writes the same second byte for every result; the
	// outcome is in the message key.
	if _, message := reply(); message != "BUY_FAIL_NOITEM" {
		t.Fatal("purchase error advertised success", message)
	}
	for _, data := range [][]byte{{1, 56}, {1, 0, 56}, {1, 56, 0, 0}, {1, 56, 0, 0, 0, 0}} {
		if err = s.shopRequest(c, &protocol.Reader{Data: data}); err == nil {
			t.Fatal("malformed purchase accepted", data)
		}
	}
	ch, _ = db.Character(1)
	if ch.Points != 3 {
		t.Fatal("malformed request debited account", ch)
	}
	s.shop.Products[0].Subs = append(s.shop.Products[0].Subs, subproduct{ID: 57, Price: 1, Items: []store.Reward{{ID: 212, Count: 1, Duration: -1}}})
	if err = s.shopRequest(c, &protocol.Reader{Data: []byte{1, 57, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if success, message := reply(); success != 1 || message != "BUY_OK" {
		t.Fatal("current catalog permanent item rejected", success, message)
	}
	items, err = db.Inventory(1)
	// A permanent copy carries no duration, so it is inserted rather than folded
	// into the timed copy: bag = default, timed weapon, selected permer. The
	// reference clamps the negative duration to zero and marks the first copy
	// as selected (itemmanager.cpp:683,669), so the new row is 1/1 like any
	// other first copy.
	if err != nil || len(items) != 3 || items[2].InUse != 1 || items[2].Expiry != 0 || items[2].Status != 1 {
		t.Fatal("permanent catalog item was not stored as selected", items, err)
	}
}
