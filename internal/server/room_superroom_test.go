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
	if err = s.refreshSuperRoom(c, &st); err != nil {
		t.Fatal(err)
	}
	if st.number(58) != 0 {
		t.Fatal("client-supplied superRoom flag was trusted")
	}
	if st.flags[1]&(1<<26) == 0 {
		t.Fatal("superRoom flag bit was not sent")
	}

	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,20,8357,1,0,0,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	if err = s.refreshSuperRoom(c, &st); err != nil {
		t.Fatal(err)
	}
	if st.number(58) != 1 {
		t.Fatal("owned superRoom item did not set the flag")
	}
}
