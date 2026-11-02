package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestRoomUnk32And33Replies(t *testing.T) {
	for _, kind := range []byte{32, 33} {
		c := &client{queue: make(chan outbound, 4)}
		s := &Server{}
		if err := s.roomRequest(c, &protocol.Reader{Data: []byte{kind}}); err != nil {
			t.Fatal(err)
		}
		message := <-c.queue
		if message.id != 65 {
			t.Fatalf("subtype %d packet id %d", kind, message.id)
		}
		want := []byte{kind, 0, 0, 0, 0, 0}
		if string(message.data) != string(want) {
			t.Fatalf("subtype %d payload %v", kind, message.data)
		}
	}
}

func TestRoomSetZBAddonsConsumesItems(t *testing.T) {
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
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(1,10,1234,1,0,0,0,0,0,0,0,0,0,0,0,0)"); err != nil {
		t.Fatal(err)
	}
	c := &client{
		user: store.User{
			ID:   1,
			Name: "addonuser",
		},
		queue: make(chan outbound, 8),
	}
	s := &Server{
		Store: db,
		items: map[int]itemDefinition{
			1234: {
				ClassName: "zbsaddonitem",
			},
		},
		clients: map[*client]bool{c: true},
		users:   map[int]*client{1: c},
		rooms:   map[int]*room{7: {id: 7}},
	}
	c.room = 7
	r := &protocol.Reader{Data: []byte{1, 0, 0xd2, 0x04}}
	if err = s.setZBAddons(c, r); err != nil {
		t.Fatal(err)
	}
	addons, err := db.Addons(1)
	if err != nil || len(addons) != 1 || addons[0] != 1234 {
		t.Fatalf("stored addons %v, %v", addons, err)
	}
	items, err := db.Inventory(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == 1234 {
			t.Fatal("addon item was not consumed")
		}
	}
	message := <-c.queue
	if message.id != 154 {
		t.Fatalf("inventory update packet %d", message.id)
	}
}

func TestRoomKickClanRemovesOpposingClan(t *testing.T) {
	s, a, b, c, d := familyBattleSetup(t)
	r := &room{
		id:       7,
		host:     a,
		members:  []*client{a, d, b},
		settings: familySettings(),
	}
	if !s.familyBattleInit(r, a) || !s.familyBattleJoin(r, b) {
		t.Fatal("clan slots were not assigned")
	}
	s.rooms = map[int]*room{7: r}
	for _, member := range r.members {
		member.room = 7
	}
	if err := s.kickClanMembers(a); err != nil {
		t.Fatal(err)
	}
	if len(r.members) != 2 || r.members[0] != a || r.members[1] != d {
		t.Fatalf("remaining members %v", r.members)
	}
	if !r.kicked[2] {
		t.Fatal("kicked user was not recorded")
	}
	if !b.inLobby || b.room != 0 {
		t.Fatalf("kicked user not returned to lobby: %+v", b)
	}
	if len(c.queue) != 0 {
		t.Fatal("clanless spectators should not be kicked")
	}
	var message outbound
	for i := 0; i < 16; i++ {
		next := <-b.queue
		if next.id == 65 && len(next.data) > 0 && next.data[0] == 37 {
			message = next
			break
		}
	}
	if message.id != 65 {
		t.Fatalf("kick packet %d", message.id)
	}
	reader := &protocol.Reader{Data: message.data}
	if reader.U8() != 37 || reader.U8() != 1 || reader.U32() != 2 {
		t.Fatalf("kick payload %v", message.data)
	}
}

func TestRoomKickClanRequiresFamilyBattleHost(t *testing.T) {
	s, a, b, _, _ := familyBattleSetup(t)
	r := &room{
		id:       7,
		host:     a,
		members:  []*client{a, b},
		settings: settings{},
	}
	s.rooms = map[int]*room{7: r}
	a.room = 7
	b.room = 7
	if err := s.kickClanMembers(a); err != nil {
		t.Fatal(err)
	}
	if len(r.members) != 2 {
		t.Fatal("non-family room kicked a member")
	}
	if err := s.kickClanMembers(b); err != nil {
		t.Fatal(err)
	}
	if len(r.members) != 2 {
		t.Fatal("non-host kicked a member")
	}
}

func voxelSettings(id string) settings {
	s := settings{}
	b := []byte{1, 0, 0, 0}
	b = append(b, []byte(id)...)
	b = append(b, 0)
	s.fields[50] = b
	return s
}

func TestRoomVoxelListFiltersBySlotID(t *testing.T) {
	c := &client{queue: make(chan outbound, 8)}
	s := &Server{rooms: map[int]*room{
		1: {
			id: 1,
		},
		3: {
			id:       3,
			settings: voxelSettings("slot-a"),
		},
		5: {
			id:       5,
			settings: voxelSettings("slot-b"),
		},
	}}
	request := append([]byte{34}, []byte("slot-a")...)
	request = append(request, 0)
	if err := s.roomRequest(c, &protocol.Reader{Data: request}); err != nil {
		t.Fatal(err)
	}
	message := <-c.queue
	if message.id != 65 || message.data[0] != 34 || message.data[1] != 1 {
		t.Fatalf("voxel list %v", message.data)
	}
	reader := &protocol.Reader{Data: message.data[2:]}
	if reader.U8() != 0 || reader.U32() != 3 {
		t.Fatalf("voxel list entry %v", message.data)
	}
}
