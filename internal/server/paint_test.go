package server

import (
	"encoding/binary"
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func paintPacket(kind, weaponSlot, value int) []byte {
	data := []byte{byte(kind)}
	data = binary.LittleEndian.AppendUint16(data, uint16(weaponSlot))
	data = binary.LittleEndian.AppendUint16(data, uint16(value))
	return data
}

func TestWeaponPaintApplyAndSwitch(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "paint.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("paintuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "PaintUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	now := int(time.Now().Unix() / 60)
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,4,1,1,1,?,0,0,0,0,0,0,0,0,0)", now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,3,744,1,1,1,?,0,0,0,0,0,0,0,0,0)", now); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{InventorySlotMax: 10}, weaponPaints: map[int][]int{4: {744}}}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "PaintUser"}, queue: make(chan outbound, 16)}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: paintPacket(17, 2, 3)}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 154 {
		t.Fatalf("missing paint inventory update, got %d", msg.id)
	}
	msg := <-c.queue
	if msg.id != 78 || len(msg.data) != 4 || msg.data[0] != 17 || msg.data[1] != 1 {
		t.Fatalf("unexpected paint reply %v", msg.data)
	}
	var paint int
	var list string
	if err = db.DB.QueryRow("SELECT paintID,paintIDList FROM UserInventory WHERE userID=1 AND slot=2").Scan(&paint, &list); err != nil {
		t.Fatal(err)
	}
	if paint != 744 || list != "744" {
		t.Fatalf("stored paint %d %q", paint, list)
	}
	var rows int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND slot=3").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatal("paint item was not consumed")
	}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: paintPacket(24, 2, 744)}); err != nil {
		t.Fatal(err)
	}
	if msg = <-c.queue; msg.id != 154 {
		t.Fatalf("missing switch inventory update, got %d", msg.id)
	}
	if msg = <-c.queue; msg.id != 78 || msg.data[0] != 17 {
		t.Fatalf("unexpected switch reply %v", msg.data)
	}
	if err = s.dispatch(c, protocol.Packet{ID: 78, Data: paintPacket(24, 2, 0)}); err != nil {
		t.Fatal(err)
	}
	if msg = <-c.queue; msg.id != 154 {
		t.Fatalf("missing remove-paint inventory update, got %d", msg.id)
	}
	if err = db.DB.QueryRow("SELECT paintID FROM UserInventory WHERE userID=1 AND slot=2").Scan(&paint); err != nil {
		t.Fatal(err)
	}
	if paint != 0 {
		t.Fatalf("paint removal left %d", paint)
	}
}
