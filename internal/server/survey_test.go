package server

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func newSurveyTestServer(t *testing.T) (*Server, *client, *store.Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "survey.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("surveyuser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "SurveyUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	surveys, err := loadSurveys(map[string]config.Survey{
		"1": {
			Title: "User survey",
			Questions: map[string]config.SurveyQuestion{
				"1": {Question: "Do you like CSN:S?", AnswerCheckBoxType: 1, Answers: []config.SurveyAnswer{{ID: 1, Answer: "Yes"}, {ID: 2, Answer: "No"}}},
				"2": {Question: "Comment", AnswerType: 1, TextEntryUnk: 7},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Store:   db,
		Config:  config.Config{},
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		surveys: surveys,
	}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "SurveyUser"}, queue: make(chan outbound, 16)}
	return s, c, db
}

func TestSurveySendAndAnswer(t *testing.T) {
	s, c, db := newSurveyTestServer(t)
	if err := s.sendSurveys(c); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	if msg.id != 87 {
		t.Fatalf("survey packet id %d", msg.id)
	}
	r := &protocol.Reader{Data: msg.data}
	if r.U8() != 0 || r.U32() != 1 || r.CString() != "User survey" || r.U8() != 2 {
		t.Fatalf("unexpected survey header: %v at %d", r.Err, r.Pos)
	}
	if r.U8() != 1 || r.CString() != "Do you like CSN:S?" || r.U8() != 0 || r.U8() != 1 || r.U8() != 2 {
		t.Fatalf("unexpected question 1: %v at %d", r.Err, r.Pos)
	}
	if r.U8() != 1 || r.CString() != "Yes" || r.U8() != 2 || r.CString() != "No" {
		t.Fatalf("unexpected answer list: %v at %d", r.Err, r.Pos)
	}
	if r.U8() != 2 || r.CString() != "Comment" || r.U8() != 1 || r.U16() != 7 {
		t.Fatalf("unexpected question 2: %v at %d", r.Err, r.Pos)
	}
	if r.Err != nil || r.Pos != len(r.Data) {
		t.Fatalf("survey packet tail: %v at %d/%d", r.Err, r.Pos, len(r.Data))
	}

	// Survey 1: answer the checkbox question and the text-entry question.
	answer := []byte{11, 1, 0, 0, 0, 2, 1, 0, 1, 1, 2, 1, 'H', 'e', 'l', 'l', 'o', 0}
	if err := s.dispatch(c, protocol.Packet{ID: 87, Data: answer}); err != nil {
		t.Fatal(err)
	}
	reply := <-c.queue
	if reply.id != 87 || len(reply.data) != 2 || reply.data[0] != 11 || reply.data[1] != 0 {
		t.Fatalf("unexpected survey reply %d %v", reply.id, reply.data)
	}
	var rows int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserSurveyAnswer WHERE userID=1 AND surveyID=1").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("survey answer rows %d", rows)
	}
	var answerText string
	if err := db.DB.QueryRow("SELECT answer FROM UserSurveyAnswer WHERE userID=1 AND surveyID=1 AND questionID=2").Scan(&answerText); err != nil {
		t.Fatal(err)
	}
	if answerText != "Hello" {
		t.Fatalf("text answer %q", answerText)
	}

	if err := s.sendSurveys(c); err != nil {
		t.Fatal(err)
	}
	if len(c.queue) != 0 {
		t.Fatalf("answered survey was sent again (%d packets)", len(c.queue))
	}
}

func TestSurveyRejectsUnknownAnswers(t *testing.T) {
	s, c, db := newSurveyTestServer(t)
	// Survey 9 does not exist.
	if err := s.dispatch(c, protocol.Packet{ID: 87, Data: []byte{11, 9, 0, 0, 0, 1, 1, 0, 1, 1}}); err != nil {
		t.Fatal(err)
	}
	if reply := <-c.queue; len(reply.data) != 2 || reply.data[1] != 1 {
		t.Fatalf("unknown survey reply %v", reply.data)
	}
	// Answer ID 9 does not exist.
	if err := s.dispatch(c, protocol.Packet{ID: 87, Data: []byte{11, 1, 0, 0, 0, 1, 1, 0, 1, 9}}); err != nil {
		t.Fatal(err)
	}
	if reply := <-c.queue; len(reply.data) != 2 || reply.data[1] != 1 {
		t.Fatalf("unknown answer reply %v", reply.data)
	}
	var rows int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserSurveyAnswer WHERE userID=1").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("invalid answers wrote %d rows", rows)
	}
}
