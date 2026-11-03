package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestSuperRoomFlagComesFromHostItem(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "super.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("superuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "SuperUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	c := &client{user: store.User{ID: 1}, queue: make(chan outbound, 4)}
	s := &Server{Store: db}

	st := settings{}
	st.setNumber(58, 1, 1)
	if err = s.refreshHostRoomItems(c, &st); err != nil {
		t.Fatal(err)
	}
	if st.number(58) != 0 {
		t.Fatal("client-supplied superRoom flag was trusted")
	}
	if st.flags[1]&(1<<26) == 0 {
		t.Fatal("superRoom flag bit was not sent")
	}

	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,20,8357,1,1,1,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	if err = s.refreshHostRoomItems(c, &st); err != nil {
		t.Fatal(err)
	}
	// The reference resolves these with GetFirstActiveItemByItemID, so the item
	// has to be selected and active (status=1 AND inUse=1), not merely owned.
	if st.number(58) != 1 {
		t.Fatal("active superRoom item did not set the flag")
	}

	host := settings{}
	host.setNumber(6, 3, 1)
	host.setNumber(33, 1, 1)
	host.setNumber(44, 1, 1)
	if err = s.refreshHostRoomItems(c, &host); err != nil {
		t.Fatal(err)
	}
	if host.number(33) != 0 || host.number(44) != 0 {
		t.Fatal("unowned C4 sound or big-head item kept")
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,21,112,1,1,1,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,22,439,1,1,1,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	host.setNumber(33, 1, 1)
	if err = s.refreshHostRoomItems(c, &host); err != nil {
		t.Fatal(err)
	}
	if host.number(33) != 1 || host.number(44) != 1 {
		t.Fatal("active C4 sound or big-head item not applied")
	}
	clear := settings{}
	clear.setNumber(6, 0, 1)
	clear.setNumber(44, 1, 1)
	if err = s.refreshHostRoomItems(c, &clear); err != nil {
		t.Fatal(err)
	}
	// CheckForHostItems writes the field with value 0 for modes that do not use
	// it, so the field stays in the frame instead of disappearing.
	if !clear.hasField(44) || clear.number(44) != 0 {
		t.Fatalf("big-head field for an unused mode: present=%v value=%d", clear.hasField(44), clear.number(44))
	}
}
