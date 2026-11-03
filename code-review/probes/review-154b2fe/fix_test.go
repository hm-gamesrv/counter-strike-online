package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestReview154RepeatedAddonCannotReuseDeletedStack(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	c.room = 1
	s.rooms = map[int]*room{1: {id: 1}}
	s.items[7001] = itemDefinition{ClassName: "zbsaddonitem"}
	if _, err := db.GrantReward(1, store.Grant{Items: []store.Reward{{
		ID:         7001,
		Count:      1,
		Unselected: true,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	w := &protocol.Writer{}
	w.U16(6)
	for range 6 {
		w.U16(7001)
	}
	if err := s.setZBAddons(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	addons, err := db.Addons(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(addons) != 1 {
		t.Fatalf("one owned unit paid for %d addon entries: %v", len(addons), addons)
	}
}
