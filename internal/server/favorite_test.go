package server

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func fastBuyPacket(slot int, name string) []byte {
	data := []byte{1}
	data = append(data, byte(slot))
	data = append(data, []byte(name)...)
	data = append(data, 0)
	for i := 1; i <= 11; i++ {
		data = binary.LittleEndian.AppendUint16(data, uint16(i))
	}
	return data
}

func TestFastBuyPresetUpsert(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "fastbuy.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("fastuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "FastUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "FastUser"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 77, Data: fastBuyPacket(2, "Preset")}); err != nil {
		t.Fatal(err)
	}
	var name, items string
	if err = db.DB.QueryRow("SELECT name,items FROM UserFastBuy WHERE userID=1 AND fastBuyID=2").Scan(&name, &items); err != nil {
		t.Fatal(err)
	}
	if name != "Preset" || items != "1,2,3,4,5,6,7,8,9,10,11" {
		t.Fatalf("fastbuy %q %q", name, items)
	}
	if err = s.dispatch(c, protocol.Packet{ID: 77, Data: fastBuyPacket(2, "Second")}); err != nil {
		t.Fatal(err)
	}
	var rows int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserFastBuy WHERE userID=1 AND fastBuyID=2").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("fastbuy rows %d", rows)
	}
}

func TestAddonListPacket(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "addon.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("addonuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "AddonUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserAddon(userID,itemID) VALUES(1,100),(1,200)"); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "AddonUser"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 115}); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	r := &protocol.Reader{Data: msg.data}
	if msg.id != 115 || r.U16() != 2 || r.U16() != 100 || r.U16() != 200 {
		t.Fatalf("unexpected addon packet %v", msg.data)
	}
}
