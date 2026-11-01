package store

import (
	"bytes"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func TestQuestCompletionAndAwardSurviveFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "quests.db3")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err = s.Register("quest", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateCharacter(1, "Quest", config.DefaultUser{Level: 1, Points: 100}); err != nil {
		t.Fatal(err)
	}
	p, err := s.RecordQuestProgressOnce(1, 1, 1, 3, 1, 20, nil, "match/minute/1")
	if err != nil || p.Units != 1 {
		t.Fatal(p, err)
	}
	p, err = s.RecordQuestProgressOnce(1, 1, 1, 3, 1, 20, nil, "match/minute/1")
	if err != nil || p.Units != 1 {
		t.Fatal("event applied twice", p, err)
	}
	// A failed queue insert must also roll back the progress and event key.
	if _, err = s.DB.Exec("CREATE TRIGGER failQuest BEFORE INSERT ON GoRewardClaim BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordQuestProgressOnce(1, 1, 1, 3, 5, 20, []byte("frozen"), "finish"); err == nil {
		t.Fatal("queue failure accepted")
	}
	p, err = s.QuestProgress(1, 1, 1)
	if err != nil || p.Units != 1 || p.Token != 0 {
		t.Fatal("queue failure changed progress", p, err)
	}
	if _, err = s.DB.Exec("DROP TRIGGER failQuest"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.RecordQuestProgressOnce(1, 1, 1, 3, 5, 20, []byte("frozen"), "finish")
			failures <- err
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err = s.QuestProgress(1, 1, 1)
	if err != nil || p.Units != 3 || p.Token == 0 {
		t.Fatal(p, err)
	}
	claims, err := s.PendingRewardClaims(1)
	if err != nil || len(claims) != 1 || !bytes.Equal(claims[0].Payload, []byte("frozen")) {
		t.Fatal("completion not frozen exactly once", claims, err)
	}
	if _, err = s.RecordQuestProgress(1, 1, 1, 4, 1, 20, nil); err == nil {
		t.Fatal("goal change reset completed task")
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g := Grant{ClaimToken: p.Token, Points: 50, Items: []Reward{{ID: 12, Count: 1}}}
	if _, err = s.GrantReward(1, g, 1, 0); err == nil {
		t.Fatal("full inventory accepted task reward")
	}
	if _, err = s.PendingRewardClaim(1, p.Token); err != nil {
		t.Fatal("failed award lost completion", err)
	}
	if _, err = s.GrantReward(1, g, 10, 0); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GrantReward(1, g, 10, 0); !errors.Is(err, ErrRewardClaimed) {
		t.Fatal("replayed task award", err)
	}
	p, err = s.RecordQuestProgress(1, 1, 1, 3, 10, 20, []byte("reroll"))
	if err != nil {
		t.Fatal(err)
	}
	claims, err = s.PendingRewardClaims(1)
	if err != nil || len(claims) != 0 {
		t.Fatal("completed task requeued", claims, err)
	}
	ch, err := s.Character(1)
	if err != nil || ch.Points != 150 {
		t.Fatal("task paid twice", ch, err)
	}
	// Distinct tasks can complete with no owned source and cannot retire as stale.
	for _, task := range []int{2, 3} {
		if _, err = s.RecordQuestProgress(1, 1, task, 1, 1, 20, []byte("other")); err != nil {
			t.Fatal(err)
		}
	}
	claims, err = s.PendingRewardClaims(1)
	if err != nil || len(claims) != 2 {
		t.Fatal("task claims collided or retired", claims, err)
	}
}

func TestQuestEventsCommitWithCharacterAndMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.db3")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err = s.Register("events", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateCharacter(1, "Events", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("CREATE TRIGGER failEvent BEFORE INSERT ON GoQuestEventOutbox BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GrantReward(1, Grant{Exp: 250, Points: 100}, 10, 0); err == nil {
		t.Fatal("event write failure allowed character change")
	}
	ch, _ := s.Character(1)
	if ch.Exp != 0 || ch.Points != 0 || ch.Level != 1 {
		t.Fatal("event failure left partial reward", ch)
	}
	reward := MatchReward{UserID: 1, Exp: 250, Points: 100, QuestMatchEnd: true, Won: true, Mode: 4, Map: 277, Players: 1}
	if err = s.SettleMatch("event-match", []MatchReward{reward}); err == nil {
		t.Fatal("event write failure allowed match settlement")
	}
	if _, err = s.DB.Exec("DROP TRIGGER failEvent"); err != nil {
		t.Fatal(err)
	}
	if err = s.SettleMatch("event-match", []MatchReward{reward}); err != nil {
		t.Fatal(err)
	}
	if err = s.SettleMatch("event-match", []MatchReward{reward}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.QuestEvents(1)
	if err != nil || len(events) != 2 {
		t.Fatal("match retry duplicated or lost events", events, err)
	}
	if events[0].Kind != 5 || events[0].Count != 2 || events[1].Kind != 1 || !events[1].Won || events[1].Mode != 4 || events[1].Map != 277 || events[1].Players != 1 {
		t.Fatal("incorrect durable match context", events)
	}
}
