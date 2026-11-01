package server

import (
	"encoding/binary"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestStarterGiftUseLoadsCurrentResourcesAndConsumesOnce(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(dir, "ServerConfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "test.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("giftuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "GiftUser", cfg.DefaultUser); err != nil {
		t.Fatal(err)
	}
	s, err := New(dir, cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "GiftUser"}, queue: make(chan outbound, 128)}
	owned, err := db.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	source := 0
	for _, item := range owned {
		if item.ID == 1001 {
			source = item.Slot
		}
	}
	request := func(count int, tail bool) error {
		w := &protocol.Writer{}
		w.U8(0)
		w.U8(3)
		w.U16(source + len(cfg.DefaultUser.DefaultItems))
		w.U8(0)
		w.U16(count)
		if tail {
			w.U8(0)
		}
		return s.itemRequest(c, &protocol.Reader{Data: w.Data})
	}
	if err = request(1, true); err == nil {
		t.Fatal("malformed use accepted")
	}
	if err = request(0, false); err == nil {
		t.Fatal("zero-count use accepted")
	}
	if err = request(201, false); err == nil {
		t.Fatal("batch larger than current client limit accepted")
	}
	if err = request(1, false); err != nil {
		t.Fatal(err)
	}
	items, err := db.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	foundNext := false
	for _, item := range items {
		if item.ID == 1001 {
			t.Fatal("starter gift not consumed")
		}
		if item.ID == 1002 {
			foundNext = true
		}
	}
	if !foundNext {
		t.Fatal("next level gift missing")
	}
	removed, notice := false, false
	for len(c.queue) > 0 {
		msg := <-c.queue
		if msg.id == 154 && len(msg.data) >= 9 && binary.LittleEndian.Uint16(msg.data[6:8]) == uint16(source+len(cfg.DefaultUser.DefaultItems)) && msg.data[8] == 0 {
			removed = true
		}
		if msg.id == 67 && len(msg.data) > 0 && msg.data[0] == 60 {
			notice = true
		}
	}
	if !removed || !notice {
		t.Fatal("missing inventory removal or localized reward notice", removed, notice)
	}
	before := len(items)
	if err = request(1, false); err != nil {
		t.Fatal(err)
	}
	items, err = db.Inventory(1)
	if err != nil || len(items) != before {
		t.Fatal("replayed gift use granted again", err)
	}
	for _, item := range items {
		if item.ID == 1002 {
			source = item.Slot
		}
	}
	if s.items[1002].Level != 3 {
		t.Fatal("current client's level-three gift gate changed")
	}
	if err = request(1, false); err != nil {
		t.Fatal(err)
	}
	items, err = db.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	foundNext = false
	for _, item := range items {
		if item.ID == 1002 {
			foundNext = true
		}
	}
	if !foundNext {
		t.Fatal("low-level request consumed the next gift")
	}
	if _, err = db.DB.Exec("UPDATE UserCharacter SET level=3 WHERE userID=1"); err != nil {
		t.Fatal(err)
	}
	if err = request(1, false); err != nil {
		t.Fatal(err)
	}
	items, err = db.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	foundNext = false
	for _, item := range items {
		if item.ID == 1002 {
			t.Fatal("client level-three gift rejected by stale reward metadata")
		}
		if item.ID == 1003 {
			foundNext = true
		}
	}
	if !foundNext {
		t.Fatal("next gift tier missing")
	}
}
