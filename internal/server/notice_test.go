package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestSendNotices(t *testing.T) {
	notices, err := loadNotices(map[string]config.Notice{
		"2": {
			Type:        1,
			Name:        "Second",
			Description: "Body",
			StartDate:   120,
			EndDate:     240,
		},
		"1": {
			Type:        0,
			Name:        "First",
			Description: "Hello",
			StartDate:   60,
			EndDate:     180,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		notices: notices,
	}
	c := &client{
		user: store.User{
			ID: 1,
		},
		queue: make(chan outbound, 4),
	}
	s.sendNotices(c)
	for _, want := range []string{"First", "Second"} {
		msg := <-c.queue
		if msg.id != 67 {
			t.Fatalf("notice packet id %d", msg.id)
		}
		r := &protocol.Reader{Data: msg.data}
		if subtype := r.U8(); subtype != 65 {
			t.Fatalf("notice subtype %d", subtype)
		}
		if r.U16() < 10001 {
			t.Fatal("notice id was not offset")
		}
		r.U8()
		if r.CString() != noticeURL {
			t.Fatal("unexpected notice URL")
		}
		r.U32()
		r.U32()
		if r.CString() != want {
			t.Fatalf("unexpected notice order, want %s", want)
		}
		if r.CString() == "" {
			t.Fatal("missing notice description")
		}
	}
}

func TestLoadNoticesRejectsInvalidType(t *testing.T) {
	if _, err := loadNotices(map[string]config.Notice{
		"1": {
			Type: 9,
			Name: "Bad",
		},
	}); err == nil {
		t.Fatal("invalid notice type was accepted")
	}
}
