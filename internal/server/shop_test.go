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
	if ch.Points != 3 || len(items) != 3 || items[1].ID != 212 || items[1].Status != 0 || items[2].Status != 1 || items[2].InUse != 1 || items[2].Expiry < now+4320-1 || items[2].Expiry > now+4320+1 {
		t.Fatal("purchase did not debit and grant timed weapon", ch, items)
	}
	if err = s.shopRequest(c, &protocol.Reader{Data: []byte{1, 255, 255, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if success, message := reply(); success != 0 || message != "BUY_FAIL_NOITEM" {
		t.Fatal("purchase error advertised success", success, message)
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
	if err != nil || len(items) != 4 || items[3].InUse != 0 || items[3].Expiry != 0 || items[3].Status != 0 {
		t.Fatal("permanent catalog item lost dormant state", items, err)
	}
}
