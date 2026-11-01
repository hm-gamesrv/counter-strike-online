package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestBanListAddRemoveSettingsAndLimit(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "ban.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, name := range []string{"blocker", "target", "other", "third"} {
		if err = db.Register(name, "secret1", "127.0.0.1", 0); err != nil {
			t.Fatal(err)
		}
	}
	for id, name := range []string{"Blocker", "Target", "Other", "Third"} {
		if err = db.CreateCharacter(id+1, name, config.DefaultUser{Level: 1}); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{Store: db, Config: config.Config{BanListMaxSize: 2}}
	c := &client{user: store.User{ID: 1}, character: store.Character{Name: "Blocker"}, queue: make(chan outbound, 32)}
	request := func(data []byte) outbound {
		t.Helper()
		if err := s.dispatch(c, protocol.Packet{ID: 74, Data: data}); err != nil {
			t.Fatal(err)
		}
		return <-c.queue
	}
	add := func(name string) outbound {
		return request(append([]byte{0}, append([]byte(name), 0)...))
	}
	remove := func(name string) outbound {
		return request(append([]byte{1}, append([]byte(name), 0)...))
	}
	notice := func(msg outbound, want string) {
		t.Helper()
		r := &protocol.Reader{Data: msg.data}
		if msg.id != 67 || r.U8() != 10 || r.CString() != want {
			t.Fatalf("unexpected notice %d %v", msg.id, msg.data)
		}
	}
	reply := func(msg outbound, kind int, want string) {
		t.Helper()
		r := &protocol.Reader{Data: msg.data}
		if msg.id != 74 || r.U8() != kind || r.CString() != want {
			t.Fatalf("unexpected ban reply %d %v", msg.id, msg.data)
		}
	}
	reply(add("Target"), 1, "Target")
	notice(add("Target"), "BAN_ADD_FAIL_ID_EXIST")
	notice(add("Missing"), "BAN_ADD_FAIL_NICKNAME_NOT_EXIST")
	names, err := db.BanList(1)
	if err != nil || len(names) != 1 || names[0] != "Target" {
		t.Fatalf("ban list %v, %v", names, err)
	}
	msg := request([]byte{2, 0})
	r := &protocol.Reader{Data: msg.data}
	if msg.id != 74 || r.U8() != 3 || r.U8() != 0 {
		t.Fatalf("unexpected settings reply %v", msg.data)
	}
	if settings, err := db.BanSettings(1); err != nil || settings != 0 {
		t.Fatalf("ban settings %d, %v", settings, err)
	}
	msg = request([]byte{4})
	r = &protocol.Reader{Data: msg.data}
	if msg.id != 74 || r.U8() != 4 || r.U16() != 2 {
		t.Fatalf("unexpected max-size reply %v", msg.data)
	}
	reply(add("Other"), 1, "Other")
	notice(add("Third"), "BAN_ADD_FAIL_MAX_NUMBER_EXCESS")
	reply(remove("Target"), 2, "Target")
	notice(remove("Target"), "BAN_REMOVE_FAIL_NICKNAME_NOT_EXIST")
	if names, err = db.BanList(1); err != nil || len(names) != 1 || names[0] != "Other" {
		t.Fatalf("final ban list %v, %v", names, err)
	}
}

func TestSendBanLoginSendsListAndSettings(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "banlogin.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("blocker", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.Register("target", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Blocker", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(2, "Target", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if code, err := db.AddBan(1, "Target", 10); err != nil || code != 1 {
		t.Fatalf("add ban %d, %v", code, err)
	}
	s := &Server{Store: db}
	c := &client{user: store.User{ID: 1}, queue: make(chan outbound, 8)}
	if err = s.sendBanLogin(c); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	r := &protocol.Reader{Data: msg.data}
	if msg.id != 74 || r.U8() != 0 || r.U16() != 1 || r.CString() != "Target" {
		t.Fatalf("unexpected ban list login packet %v", msg.data)
	}
	msg = <-c.queue
	r = &protocol.Reader{Data: msg.data}
	if msg.id != 74 || r.U8() != 3 || r.U8() != 2 {
		t.Fatalf("unexpected ban settings login packet %v", msg.data)
	}
}
func TestBanEnforcementPolicy(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "banpolicy.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("blocker", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.Register("target", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Blocker", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(2, "Target", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if code, err := db.AddBan(1, "Target", 10); err != nil || code != 1 {
		t.Fatalf("add ban %d, %v", code, err)
	}
	s := &Server{Store: db}
	host := &client{user: store.User{ID: 1}, character: store.Character{Name: "Blocker"}}
	guest := &client{user: store.User{ID: 2}, character: store.Character{Name: "Target"}}
	if s.chatAllowed(guest, host) {
		t.Fatal("blocked sender still reached recipient chat")
	}
	if !s.chatAllowed(host, guest) {
		t.Fatal("sender without a block should not be filtered by their own list")
	}
	if err = db.SetBanSettings(1, 0); err != nil {
		t.Fatal(err)
	}
	if !s.chatAllowed(guest, host) {
		t.Fatal("chat filter ignored disabled ban settings")
	}
	if err = db.SetBanSettings(1, 3); err != nil {
		t.Fatal(err)
	}
	if allowed, err := s.hostAllows(host, guest); err != nil || allowed {
		t.Fatalf("host allowed blocked guest, allowed=%v err=%v", allowed, err)
	}
	if err = db.SetBanSettings(1, 2); err != nil {
		t.Fatal(err)
	}
	if allowed, err := s.hostAllows(host, guest); err != nil || !allowed {
		t.Fatalf("host join filter used wrong bit, allowed=%v err=%v", allowed, err)
	}
	if reason, denied := s.whisperDenied(guest, host); denied {
		t.Fatalf("guest should not have blocked host yet: %q", reason)
	}
	if code, err := db.AddBan(2, "Blocker", 10); err != nil || code != 1 {
		t.Fatalf("add reverse ban %d, %v", code, err)
	}
	if reason, denied := s.whisperDenied(guest, host); !denied || reason != "" {
		t.Fatalf("blocked whisper should be silent, reason=%q denied=%v", reason, denied)
	}
	if err = db.SetBanSettings(2, 6); err != nil {
		t.Fatal(err)
	}
	if reason, denied := s.whisperDenied(guest, host); !denied || reason != "MSG_TELL_SENDER_USING_BAN_CHAT_ALL" {
		t.Fatalf("sender block-all reason=%q denied=%v", reason, denied)
	}
	if err = db.SetBanSettings(2, 6); err != nil {
		t.Fatal(err)
	}
	if reason, denied := s.whisperDenied(host, guest); !denied || reason != "MSG_TELL_LISTENER_USING_BAN_CHAT_ALL" {
		t.Fatalf("recipient block-all reason=%q denied=%v", reason, denied)
	}
}
