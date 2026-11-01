package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestItemPacketsMapGameSlotsAndRejectMalformedRequests(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "items.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("items", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Items", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if err = db.Purchase(1, true, 0, 0, []store.Reward{{ID: 14, Count: 1}}, 100, 1); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{DefaultUser: config.DefaultUser{DefaultItems: []int{25}}}, items: map[int]itemDefinition{14: {Category: 4}}}
	c := &client{user: store.User{ID: 1}, character: store.Character{Name: "Items"}, queue: make(chan outbound, 64)}
	request := func(data []byte) error { return s.dispatch(c, protocol.Packet{ID: 78, Data: data}) }
	lock := func() int {
		var value int
		if err := db.DB.QueryRow("SELECT lockStatus FROM UserInventory WHERE userID=1 AND slot=2").Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	for _, malformed := range [][]byte{{26, 1, 3}, {26, 1, 3, 0, 0}} {
		if request(malformed) == nil || lock() != 0 {
			t.Fatal("malformed lock request changed item")
		}
	}
	if err = request([]byte{26, 1, 3, 0}); err != nil || lock() != 1 {
		t.Fatal("game slot did not map to owned slot", err)
	}
	msg := <-c.queue
	p := &protocol.Reader{Data: msg.data}
	if msg.id != 154 || p.U32() != 0 || p.U16() != 1 || p.U16() != 3 {
		t.Fatal("inventory delta used incorrect game slot")
	}
	if err = request([]byte{26, 0, 3, 0}); err != nil || lock() != 0 {
		t.Fatal("unlock failed", err)
	}
	if err = request([]byte{26, 1, 0, 0}); err != nil || lock() != 0 {
		t.Fatal("default slot changed database item", err)
	}
	c.user.ID = 2
	if err = request([]byte{26, 1, 3, 0}); err != nil || lock() != 0 {
		t.Fatal("another user changed owned item", err)
	}
}
