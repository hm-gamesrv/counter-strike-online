package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func clanTestUser(t *testing.T, db *store.Store, name string, points int) int {
	t.Helper()
	if err := db.Register(name, "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	var id int
	if err := db.DB.QueryRow("SELECT userID FROM User WHERE userName=?", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateCharacter(id, name, config.DefaultUser{Level: 1, Points: points}); err != nil {
		t.Fatal(err)
	}
	return id
}

func clanTestClient(t *testing.T, db *store.Store, id int, name string) *client {
	t.Helper()
	character, err := db.Character(id)
	if err != nil {
		t.Fatal(err)
	}
	return &client{
		user: store.User{
			ID:   id,
			Name: name,
		},
		character: character,
		queue:     make(chan outbound, 64),
	}
}

func clanDispatch(t *testing.T, s *Server, c *client, data []byte) []outbound {
	t.Helper()
	if err := s.dispatch(c, protocol.Packet{ID: 71, Data: data}); err != nil {
		t.Fatal(err)
	}
	var out []outbound
	for {
		select {
		case message := <-c.queue:
			out = append(out, message)
		default:
			return out
		}
	}
}

func clanFirst(t *testing.T, messages []outbound) outbound {
	t.Helper()
	if len(messages) == 0 {
		t.Fatal("no clan packet")
	}
	return messages[0]
}

func clanFind(messages []outbound, kind int) (outbound, bool) {
	for _, message := range messages {
		r := &protocol.Reader{Data: message.data}
		if message.id == 71 && r.U8() == kind {
			return message, true
		}
	}
	return outbound{}, false
}

func TestClanCreateJoinAndList(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "clan.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	masterID := clanTestUser(t, db, "master", 40000)
	targetID := clanTestUser(t, db, "target", 0)
	master := clanTestClient(t, db, masterID, "master")
	target := clanTestClient(t, db, targetID, "target")
	s := &Server{
		Store:   db,
		clients: map[*client]bool{},
		users:   map[int]*client{masterID: master, targetID: target},
	}
	s.clients[master] = true
	s.clients[target] = true
	messages := clanDispatch(t, s, master, append([]byte{2}, append([]byte("Alpha"), 0)...))
	first := clanFirst(t, messages)
	r := &protocol.Reader{Data: first.data}
	if first.id != 71 || r.U8() != 2 || r.U8() != 1 {
		t.Fatalf("create reply %d %v", first.id, first.data)
	}
	if _, ok := clanFind(messages, 107); !ok {
		t.Fatal("missing clan update after create")
	}
	character, err := db.Character(masterID)
	if err != nil || character.ClanID != 1 || character.Points != 10000 {
		t.Fatalf("master after create %+v, %v", character, err)
	}
	messages = clanDispatch(t, s, master, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0})
	list := clanFirst(t, messages)
	r = &protocol.Reader{Data: list.data}
	if list.id != 71 || r.U8() != 0 || r.U8() != 1 || r.U16() != 0 || r.U16() != 1 || r.U32() != 1 {
		t.Fatalf("clan list %v", list.data)
	}
	messages = clanDispatch(t, s, target, []byte{3, 1, 0, 0, 0})
	join := clanFirst(t, messages)
	r = &protocol.Reader{Data: join.data}
	if join.id != 71 || r.U8() != 3 || r.U8() != 0 {
		t.Fatalf("join reply %v", join.data)
	}
	character, err = db.Character(targetID)
	if err != nil || character.ClanID != 1 {
		t.Fatalf("target after join %+v, %v", character, err)
	}
	if target.character.ClanName != "Alpha" {
		t.Fatalf("target clan name %q", target.character.ClanName)
	}
}

func TestClanApprovalChatAndKick(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "clan.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	masterID := clanTestUser(t, db, "master", 40000)
	targetID := clanTestUser(t, db, "target", 0)
	master := clanTestClient(t, db, masterID, "master")
	target := clanTestClient(t, db, targetID, "target")
	s := &Server{
		Store:   db,
		clients: map[*client]bool{},
		users:   map[int]*client{masterID: master, targetID: target},
	}
	s.clients[master] = true
	s.clients[target] = true
	clanDispatch(t, s, master, append([]byte{2}, append([]byte("Alpha"), 0)...))
	if _, err = db.DB.Exec("UPDATE Clan SET joinMethod=2 WHERE clanID=1"); err != nil {
		t.Fatal(err)
	}
	messages := clanDispatch(t, s, target, []byte{3, 1, 0, 0, 0})
	join := clanFirst(t, messages)
	r := &protocol.Reader{Data: join.data}
	if join.id != 71 || r.U8() != 3 || r.U8() != 4 || r.CString() != "CSO_CLAN_JOIN_NO_AUTH" {
		t.Fatalf("join request reply %v", join.data)
	}
	requests, err := db.ClanJoinRequests(masterID)
	if err != nil || len(requests) != 1 {
		t.Fatalf("join requests %+v, %v", requests, err)
	}
	messages = clanDispatch(t, s, master, append([]byte{5}, append([]byte("target"), 0)...))
	approve := clanFirst(t, messages)
	r = &protocol.Reader{Data: approve.data}
	if approve.id != 71 || r.U8() != 5 || r.U8() != 1 {
		t.Fatalf("approve reply %v", approve.data)
	}
	character, err := db.Character(targetID)
	if err != nil || character.ClanID != 1 {
		t.Fatalf("target after approve %+v, %v", character, err)
	}
	for len(target.queue) != 0 {
		<-target.queue
	}
	for len(master.queue) != 0 {
		<-master.queue
	}
	clanDispatch(t, s, target, append([]byte{113}, append([]byte("hello"), 0)...))
	chat, ok := clanFind(func() []outbound {
		var out []outbound
		for len(master.queue) != 0 {
			out = append(out, <-master.queue)
		}
		return out
	}(), 113)
	if !ok {
		t.Fatal("missing clan chat")
	}
	r = &protocol.Reader{Data: chat.data}
	if r.U8() != 113 || r.CString() != "target" || r.CString() != "hello" {
		t.Fatalf("chat packet %v", chat.data)
	}
	messages = clanDispatch(t, s, master, append([]byte{12}, append([]byte("target"), 0)...))
	kick := clanFirst(t, messages)
	r = &protocol.Reader{Data: kick.data}
	if kick.id != 71 || r.U8() != 12 || r.U8() != 1 {
		t.Fatalf("kick reply %v", kick.data)
	}
	if target.character.ClanID != 0 {
		t.Fatal("kicked target retained clan")
	}
	kicked := false
	for len(target.queue) != 0 {
		message := <-target.queue
		r = &protocol.Reader{Data: message.data}
		if message.id == 71 && r.U8() == 106 {
			kicked = true
		}
	}
	if !kicked {
		t.Fatal("missing kick notification")
	}
}
func TestClanStorageMoveAndDelete(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "clan.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	masterID := clanTestUser(t, db, "master", 40000)
	targetID := clanTestUser(t, db, "target", 0)
	master := clanTestClient(t, db, masterID, "master")
	target := clanTestClient(t, db, targetID, "target")
	s := &Server{
		Store: db,
		Config: config.Config{
			InventorySlotMax: 3000,
		},
		clients: map[*client]bool{},
		users:   map[int]*client{masterID: master, targetID: target},
	}
	s.clients[master] = true
	s.clients[target] = true
	clanDispatch(t, s, master, append([]byte{2}, append([]byte("Alpha"), 0)...))
	clanDispatch(t, s, target, []byte{3, 1, 0, 0, 0})
	for len(master.queue) != 0 {
		<-master.queue
	}
	for len(target.queue) != 0 {
		<-target.queue
	}
	if _, err = db.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,40,270,1,1,0,?,0,0,0,0,0,0,0,0,0)", masterID, int(time.Now().Unix()/60)); err != nil {
		t.Fatal(err)
	}
	messages := clanDispatch(t, s, master, []byte{24, 0, 0, 40, 0})
	reply, ok := clanFind(messages, 24)
	if !ok {
		t.Fatal("missing give reply")
	}
	r := &protocol.Reader{Data: reply.data}
	if reply.id != 71 || r.U8() != 24 || r.U8() != 1 {
		t.Fatalf("give reply %v", reply.data)
	}
	if _, ok := clanFind(messages, 26); !ok {
		t.Fatal("missing storage page after give")
	}
	var count int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=? AND slot=40", masterID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("inventory after give %d, %v", count, err)
	}
	messages = clanDispatch(t, s, target, []byte{25, 0, 0, 0, 0})
	reply, ok = clanFind(messages, 25)
	if !ok {
		t.Fatal("missing take reply")
	}
	r = &protocol.Reader{Data: reply.data}
	if reply.id != 71 || r.U8() != 25 || r.U8() != 1 {
		t.Fatalf("take reply %v", reply.data)
	}
	if err = db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=? AND itemID=270 AND isClanItem=1", targetID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("target inventory after take %d, %v", count, err)
	}
	if err = db.DB.QueryRow("SELECT count(*) FROM ClanStorageItem WHERE clanID=1 AND pageID=0 AND itemID<>0").Scan(&count); err != nil || count != 0 {
		t.Fatalf("storage after take %d, %v", count, err)
	}
	for len(master.queue) != 0 {
		<-master.queue
	}
	messages = clanDispatch(t, s, master, []byte{31, 0, 0, 0, 0})
	reply, ok = clanFind(messages, 31)
	if !ok {
		t.Fatal("missing delete reply")
	}
	r = &protocol.Reader{Data: reply.data}
	if reply.id != 71 || r.U8() != 31 || r.U8() != 0 {
		t.Fatalf("delete empty storage reply %v", reply.data)
	}
}
