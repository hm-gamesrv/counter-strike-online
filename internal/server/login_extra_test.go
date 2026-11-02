package server

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func newLoginExtraServer(t *testing.T) (*Server, *client, *store.Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "login.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("loginuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "LoginUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store: db,
		Config: config.Config{
			InventorySlotMax: 10,
		},
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		items: map[int]itemDefinition{
			12: {
				Category: 2,
			},
		},
		rewards: map[int]itemReward{
			3000: {
				Title: "Offline reward",
				Items: map[string]struct{ Count, Duration int }{
					"12": {
						Count: 1,
					},
				},
			},
		},
	}
	c := &client{
		user: store.User{
			ID: 1,
		},
		character: store.Character{
			ID:   1,
			Name: "LoginUser",
		},
		queue: make(chan outbound, 16),
	}
	return s, c, db
}

func TestExpiryNotices(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	if _, err := db.DB.Exec("INSERT INTO UserExpiryNotice(userID,itemID) VALUES(1,12),(1,13)"); err != nil {
		t.Fatal(err)
	}
	if err := s.sendExpiryNotices(c); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	if msg.id != 67 {
		t.Fatalf("expiry packet id %d", msg.id)
	}
	r := &protocol.Reader{Data: msg.data}
	if r.U8() != 62 || r.U8() != 2 {
		t.Fatalf("unexpected expiry header %v", msg.data)
	}
	if r.U16() != 12 || r.U8() != 0 || r.U8() != 0 {
		t.Fatal("unexpected first expiry item")
	}
	if r.U16() != 13 {
		t.Fatal("unexpected second expiry item")
	}
	var rows int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserExpiryNotice WHERE userID=1").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("expiry notices not cleared: %d", rows)
	}
}

func TestOfflineRewardNotice(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	if _, err := db.DB.Exec("INSERT INTO UserRewardNotice(userID,rewardID) VALUES(1,3000)"); err != nil {
		t.Fatal(err)
	}
	if err := s.grantRewardNotices(c); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing reward inventory update, got %d", msg.id)
	}
	if msg := <-c.queue; msg.id != 67 {
		t.Fatalf("missing reward notice, got %d", msg.id)
	}
	var count int
	if err := db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND itemID=12").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("offline reward count %d", count)
	}
	var rows int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserRewardNotice WHERE userID=1").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("reward notice not cleared: %d", rows)
	}
}

func TestSendLeague(t *testing.T) {
	s, c, _ := newLoginExtraServer(t)
	s.sendLeague(c)
	msg := <-c.queue
	if msg.id != 180 {
		t.Fatalf("league packet id %d", msg.id)
	}
	r := &protocol.Reader{Data: msg.data}
	if r.U8() != 0 || r.U8() != 5 {
		t.Fatal("unexpected league header")
	}
	for i := 0; i < 5; i++ {
		if r.U8() != i || r.U8() != 1 || r.U16() != 1 || r.U8() != 5 || r.U32() != 0 {
			t.Fatalf("unexpected league entry %d", i)
		}
	}
}
func TestExpireItemsQueuesNotice(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	now := int(time.Now().Unix() / 60)
	if _, err := db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,7,12,1,1,0,?,?,0,0,0,0,0,0,0,0)", now-100, now-1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,8,12,1,1,0,?,0,0,0,0,0,0,0,0,0)", now-100); err != nil {
		t.Fatal(err)
	}
	s.expireItems(time.Now())
	var rows int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND slot=7").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("expired item still present %d", rows)
	}
	if err := db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND slot=8").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("permanent item removed %d", rows)
	}
	if err := db.DB.QueryRow("SELECT count(*) FROM UserExpiryNotice WHERE userID=1 AND itemID=12").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows == 0 {
		t.Fatal("expiry notice was not queued")
	}
	if err := s.sendExpiryNotices(c); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	if msg.id != 67 {
		t.Fatalf("expiry notice packet id %d", msg.id)
	}
}
