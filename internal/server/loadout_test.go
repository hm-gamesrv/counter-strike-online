package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestLoadoutOwnershipCategoryAndCurrentSelection(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "loadout.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("loadout", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	defaults := config.DefaultUser{Level: 1, PseudoDefaultItems: []int{12, 15}, DefaultItems: []int{25}}
	if err = db.CreateCharacter(1, "Loadout", defaults); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{DefaultUser: defaults}, items: map[int]itemDefinition{12: {Category: 3}, 15: {Category: 4}, 19: {Category: 4}, 25: {Category: 7}, 1001: {Category: 9}}}
	c := &client{user: store.User{ID: 1}, queue: make(chan outbound, 64)}
	set := func(kind, item int) error {
		w := &protocol.Writer{}
		w.U8(2)
		w.U8(kind)
		w.U16(item)
		return s.favorite(c, &protocol.Reader{Data: w.Data})
	}
	value := func() int {
		var item int
		if err := db.DB.QueryRow("SELECT slot0 FROM UserLoadout WHERE userID=1 AND loadoutID=0").Scan(&item); err != nil {
			t.Fatal(err)
		}
		return item
	}
	if err = set(10, 15); err != nil || value() != 15 {
		t.Fatal("owned weapon not saved", err)
	}
	for _, pair := range [][2]int{{10, 19}, {10, 1001}, {20, 12}, {10, 65535}} {
		if err = set(pair[0], pair[1]); err != nil || value() != 15 {
			t.Fatal("unowned item, nonweapon or wrong loadout changed selection", pair, err)
		}
	}
	if _, err = db.DB.Exec("UPDATE UserInventory SET expiryDate=1 WHERE userID=1 AND itemID=12"); err != nil {
		t.Fatal(err)
	}
	if err = set(10, 12); err != nil || value() != 15 {
		t.Fatal("expired weapon equipped", err)
	}
	if err = set(0, 25); err != nil {
		t.Fatal(err)
	}
	var character int
	if err = db.DB.QueryRow("SELECT characterID FROM UserCharacterExtended WHERE userID=1").Scan(&character); err != nil || character != 25 {
		t.Fatal("default background character not saved", character, err)
	}
	if err = s.favorite(c, &protocol.Reader{Data: []byte{2, 10, 1}}); err == nil || value() != 15 {
		t.Fatal("truncated equip packet changed selection")
	}
}
