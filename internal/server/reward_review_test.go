package server

import (
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// rewardMatch builds a one-player match used by the reward regression tests.
func rewardMatch(t *testing.T) (*Server, *room, *matchStat) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "reward.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("rewarduser", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "RewardUser", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	c := pipeClient(t, 1, "RewardUser")
	c.playing = true
	c.team = 2
	c.room = 1
	stat := &matchStat{
		user:  c,
		class: 10,
		kills: 200,
		items: []store.Item{
			{
				ID:     10,
				InUse:  1,
				Status: 1,
			},
			{
				ID:     73,
				InUse:  1,
				Status: 1,
			},
		},
	}
	r := &room{
		id:      1,
		host:    c,
		members: []*client{c},
		match: &gameMatch{
			id:    "reward-match",
			mode:  4,
			mapID: 1,
			stats: map[int]*matchStat{
				1: stat,
			},
		},
	}
	s := &Server{
		Store: db,
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		rooms: map[int]*room{
			1: r,
		},
	}
	s.Config.GameMatch.CalcResults.GameModeCoefficient = map[string]struct {
		Exp    int
		Points int
	}{"4": {Exp: 100, Points: 100}}
	s.Config.GameMatch.CalcResults.BonusPercentage.Classes = map[string]struct {
		Exp    int
		Points int
	}{"10": {Exp: 50, Points: 50}}
	return s, r, stat
}

// TestBonusUsesUncappedBase keeps the reference order: the class bonus is
// computed from the raw coefficient product, and only the base is clamped.
func TestBonusUsesUncappedBase(t *testing.T) {
	s, r, _ := rewardMatch(t)
	if err := s.endGame(r, true); err != nil {
		t.Fatal(err)
	}
	ch, err := s.Store.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if ch.Exp != 19999 || ch.Points != 19999 {
		t.Fatalf("earned exp=%d points=%d, want 9999+20000*50/100", ch.Exp, ch.Points)
	}
}

// TestEquipmentBonusRetryIsIdempotent checks the second settlement pass after a
// failed transaction rebuilds the same percentages instead of doubling them.
func TestEquipmentBonusRetryIsIdempotent(t *testing.T) {
	s, r, stat := rewardMatch(t)
	s.Config.GameMatch.CalcResults.ItemBonusEnabled = true
	s.Config.GameMatch.CalcResults.BonusPercentage.Items = map[string]struct {
		Exp    int
		Points int
	}{"73": {Exp: 50, Points: 50}}
	if _, err := s.Store.DB.Exec("CREATE TRIGGER failReward BEFORE UPDATE ON UserCharacter BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if err := s.endGame(r, true); err == nil {
		t.Fatal("expected the injected settlement failure")
	}
	if _, err := s.Store.DB.Exec("DROP TRIGGER failReward"); err != nil {
		t.Fatal(err)
	}
	if err := s.endGame(r, true); err != nil {
		t.Fatal(err)
	}
	if stat.itemExp != 50 || stat.itemPoints != 50 {
		t.Fatalf("item percentage after retry exp=%d points=%d, want 50/50", stat.itemExp, stat.itemPoints)
	}
}

// TestReconnectDoesNotReplayOfflineMinutes pins the play-time anchor rebuilt on
// reconnect: only minutes actually spent in the match may count.
func TestReconnectDoesNotReplayOfflineMinutes(t *testing.T) {
	s, r, _ := rewardMatch(t)
	host := pipeClient(t, 2, "Host")
	host.room = 1
	host.playing = true
	r.host = host
	r.members = []*client{host}
	delete(r.match.stats, 1)
	r.settings.setNumber(8, 8, 1)
	s.users = map[int]*client{2: host}
	now := time.Now()
	s.restore = map[int]reconnectSlot{
		1: {
			roomID:  1,
			matchID: "reward-match",
			playing: true,
			expires: now.Add(time.Minute),
			hasStat: true,
			stat: reconnectStat{
				started:      now.Add(-4 * time.Minute),
				questMinutes: 1,
			},
		},
	}
	s.quests = []questTask{{QuestID: 1, TaskID: 1, Goal: 100, Conditions: []questCondition{{EventType: 0, GoalPoints: 1}}}}
	c := pipeClient(t, 1, "RewardUser")
	c.inLobby = true
	s.restoreSession(c)
	s.tickQuestTime(now)
	progress, err := s.Store.QuestProgress(1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Units != 0 {
		t.Fatalf("offline time awarded %d quest minutes", progress.Units)
	}
}
