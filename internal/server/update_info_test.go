package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestRenameCharacterConsumesNicknameItem(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "rename.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("renamer", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "OldName", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,65,2,0,1,?,0,0,0,0,0,0,0,0,0)", time.Now().Unix()/60); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{InventorySlotMax: 10}}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "OldName"}, queue: make(chan outbound, 16)}
	if err = s.dispatch(c, protocol.Packet{ID: 69, Data: []byte{2, 'N', 'e', 'w', 'N', 'a', 'm', 'e', 0}}); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	r := &protocol.Reader{Data: msg.data}
	if msg.id != 69 || r.U8() != 2 || r.U8() != 2 || r.CString() != "" {
		t.Fatalf("unexpected nickname reply %v", msg.data)
	}
	if msg = <-c.queue; msg.id != 154 {
		t.Fatalf("missing inventory update, got %d", msg.id)
	}
	if msg = <-c.queue; msg.id != 157 {
		t.Fatalf("missing user-info update, got %d", msg.id)
	}
	if c.character.Name != "NewName" {
		t.Fatalf("cached character name %q", c.character.Name)
	}
	var name string
	if err = db.DB.QueryRow("SELECT gameName FROM UserCharacter WHERE userID=1").Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "NewName" {
		t.Fatalf("stored name %q", name)
	}
	var count int
	if err = db.DB.QueryRow("SELECT count FROM UserInventory WHERE userID=1 AND itemID=65").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("nickname item count %d", count)
	}
}

func TestRenameCharacterRejectsDuplicateAndMissingItem(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "rename.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"renamer", "taken"} {
		if err = db.Register(name, "secret1", "127.0.0.1", 0); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.CreateCharacter(1, "OldName", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(2, "Taken", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,65,1,0,1,?,0,0,0,0,0,0,0,0,0)", time.Now().Unix()/60); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{InventorySlotMax: 10}}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "OldName"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 69, Data: []byte{2, 'T', 'a', 'k', 'e', 'n', 0}}); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	r := &protocol.Reader{Data: msg.data}
	if msg.id != 69 || r.U8() != 2 || r.U8() != 253 || r.CString() != "CSO_NICKNAME_ALREADY_EXIST" {
		t.Fatalf("unexpected duplicate reply %v", msg.data)
	}
	if _, err = db.DB.Exec("DELETE FROM UserInventory WHERE userID=1 AND itemID=65"); err != nil {
		t.Fatal(err)
	}
	if err = s.dispatch(c, protocol.Packet{ID: 69, Data: []byte{2, 'N', 'e', 'w', 'N', 'a', 'm', 'e', 0}}); err != nil {
		t.Fatal(err)
	}
	msg = <-c.queue
	r = &protocol.Reader{Data: msg.data}
	if msg.id != 69 || r.U8() != 2 || r.U8() != 8 || r.CString() != "CSO_NEED_NICKNAME_ITEM" {
		t.Fatalf("unexpected missing-item reply %v", msg.data)
	}
}
func TestChatColorRequiresOwnedItem(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "chatcolor.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("chatuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "ChatUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,2,9070,1,1,1,?,0,0,0,0,0,0,0,0,0)", time.Now().Unix()/60); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:  db,
		Config: config.Config{InventorySlotMax: 10},
		items:  map[int]itemDefinition{9070: {Resource: "chatcolor_influ"}},
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "ChatUser"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 69, Data: []byte{15, 0x6E, 0x23}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 157 {
		t.Fatalf("missing chat color user-info update, got %d", msg.id)
	}
	if c.character.ChatColorID != 9070 {
		t.Fatalf("cached chat color %d", c.character.ChatColorID)
	}
	var color int
	if err = db.DB.QueryRow("SELECT chatColorID FROM UserCharacter WHERE userID=1").Scan(&color); err != nil {
		t.Fatal(err)
	}
	if color != 9070 {
		t.Fatalf("stored chat color %d", color)
	}
	if err = s.dispatch(c, protocol.Packet{ID: 69, Data: []byte{15, 0x7B, 0x00}}); err != nil {
		t.Fatal(err)
	}
	if err = db.DB.QueryRow("SELECT chatColorID FROM UserCharacter WHERE userID=1").Scan(&color); err != nil {
		t.Fatal(err)
	}
	if color != 9070 {
		t.Fatalf("unowned chat color changed value to %d", color)
	}
}
func TestLocationUpdatePersistsAndBroadcasts(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "location.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("locuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "LocUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Config: config.Config{InventorySlotMax: 10}}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "LocUser"}, queue: make(chan outbound, 8)}
	if err = s.dispatch(c, protocol.Packet{ID: 69, Data: []byte{4, 1, 0, 2, 0, 3, 0}}); err != nil {
		t.Fatal(err)
	}
	if msg := <-c.queue; msg.id != 157 {
		t.Fatalf("missing location user-info update, got %d", msg.id)
	}
	if c.character.Nation != 1 || c.character.City != 2 || c.character.Town != 3 {
		t.Fatalf("cached location %d/%d/%d", c.character.Nation, c.character.City, c.character.Town)
	}
	var nation, city, town int
	if err = db.DB.QueryRow("SELECT nation,city,town FROM UserCharacter WHERE userID=1").Scan(&nation, &city, &town); err != nil {
		t.Fatal(err)
	}
	if nation != 1 || city != 2 || town != 3 {
		t.Fatalf("stored location %d/%d/%d", nation, city, town)
	}
}
