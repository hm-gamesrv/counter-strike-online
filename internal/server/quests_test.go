package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestEventQuestConfigFiltersAndMissingRewardRecovery(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	tasks, err := loadEventQuests(dir)
	if err != nil || len(tasks) != 2 {
		t.Fatal(tasks, err)
	}
	tasks[0].Goal = 2
	tasks = tasks[:1]
	db, err := store.Open(filepath.Join(dir, "test.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("events", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Events", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Config: config.Config{InventorySlotMax: 10}, quests: tasks, rewards: map[int]itemReward{}, items: map[int]itemDefinition{245: {Category: 2}}, rooms: map[int]*room{}}
	c := &client{version: true, user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "Events"}, playing: true, room: 1, queue: make(chan outbound, 128)}
	stat := &matchStat{user: c, items: []store.Item{{ID: 242, InUse: 1, Status: 1}}}
	m := &gameMatch{id: "event-test", mode: 4, mapID: 277, stats: map[int]*matchStat{1: stat}}
	r := &room{id: 1, host: c, match: m}
	s.rooms[1] = r
	event := questEvent{Kind: 2, GunID: 12, KillerTeam: 2, VictimTeam: 1}
	if err = s.applyQuestEvent(c, m, stat, event); err != nil {
		t.Fatal(err)
	}
	p, _ := db.QuestProgress(1, 1, 1)
	if p.Units != 0 {
		t.Fatal("wrong gun counted", p)
	}
	event.GunID = 242
	stat.items = nil
	if err = s.applyQuestEvent(c, m, stat, event); err != nil {
		t.Fatal(err)
	}
	p, _ = db.QuestProgress(1, 1, 1)
	if p.Units != 0 {
		t.Fatal("unowned gun counted", p)
	}
	stat.items = []store.Item{{ID: 242, InUse: 1, Status: 1}}
	// Exercise host parsing and authorization, rather than only the filter helper.
	packet := func(actor *client, truncated bool) error {
		w := &protocol.Writer{}
		w.U8(3)
		w.U32(1)
		w.U16(242)
		w.U8(2)
		w.U32(0)
		w.U8(1)
		w.U16(0)
		w.Bytes(make([]byte, 24))
		if truncated {
			w.Data = w.Data[:len(w.Data)-1]
		}
		return s.hostRequest(actor, &protocol.Reader{Data: w.Data})
	}
	if err = packet(&client{room: 1, user: store.User{ID: 2}}, false); err == nil {
		t.Fatal("non-host kill accepted")
	}
	if err = packet(c, true); err == nil {
		t.Fatal("truncated kill accepted")
	}
	if err = packet(c, false); err != nil {
		t.Fatal(err)
	}
	p, _ = db.QuestProgress(1, 1, 1)
	if p.Units != 1 {
		t.Fatal("bot kill not counted", p)
	}
	if err = packet(c, false); err != nil {
		t.Fatal(err)
	}
	p, _ = db.QuestProgress(1, 1, 1)
	if p.Units != 2 || p.Token != 0 {
		t.Fatal("missing reward lost progress or fabricated claim", p)
	}
	// Filling the omitted reward later preserves the earned completion.
	s.rewards[1111] = itemReward{Title: "Upgrade", Items: map[string]struct{ Count, Duration int }{"245": {Count: 1}}}
	c.playing = false
	if err = s.resumeQuestRewards(c); err != nil {
		t.Fatal(err)
	}
	p, _ = db.QuestProgress(1, 1, 1)
	if p.Token == 0 {
		t.Fatal("completed task not queued", p)
	}
	if err = s.resumeQuestRewards(c); err != nil {
		t.Fatal(err)
	}
	owned, _ := db.Inventory(1)
	count := 0
	for _, item := range owned {
		if item.ID == 245 {
			count++
		}
	}
	if count != 1 {
		t.Fatal("recovery did not award exactly one upgrade", owned)
	}
}

func TestQuestConditionsAndMinuteDeduplication(t *testing.T) {
	cond := questCondition{EventType: 2, GoalPoints: 1, GameModes: []int{4}, Maps: []int{277}, PlayerCount: 1, KillerTeam: 2, VictimTeam: 0, VictimKillType: 1, GunID: 242, Bot: -1, CheckForGun: true}
	c := &client{playing: true}
	stat := &matchStat{user: c, items: []store.Item{{ID: 242, InUse: 1, Status: 1}}}
	m := &gameMatch{mode: 4, mapID: 277, stats: map[int]*matchStat{1: stat}}
	e := questEvent{Kind: 2, GunID: 242, KillerTeam: 2, VictimTeam: 1, VictimKillType: 1, VictimID: 2}
	if !questConditionMatches(cond, e, m, stat) {
		t.Fatal("matching human kill rejected")
	}
	for _, mutate := range []func(*questCondition){func(c *questCondition) { c.GameModes = []int{9} }, func(c *questCondition) { c.Maps = []int{1} }, func(c *questCondition) { c.PlayerCount = 2 }, func(c *questCondition) { c.Bot = 1 }, func(c *questCondition) { c.VictimKillType = 2 }, func(c *questCondition) { c.KillerTeam = 1 }} {
		copy := cond
		mutate(&copy)
		if questConditionMatches(copy, e, m, stat) {
			t.Fatal("filter ignored", copy)
		}
	}
	e.VictimID = 0
	if !questConditionMatches(cond, e, m, stat) {
		t.Fatal("unrestricted bot filter rejected bot")
	}
	e.VictimTeam = 2
	if questConditionMatches(cond, e, m, stat) {
		t.Fatal("enemy-team requirement ignored")
	}
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "minutes.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("minutes", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Minutes", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	c.user.ID = 1
	stat.started = time.Now()
	m.id = "minutes"
	s := &Server{Store: db, Log: slog.Default(), quests: []questTask{{QuestID: 2, TaskID: 1, Goal: 10, Conditions: []questCondition{{EventType: 0, GoalPoints: 1}}}}, rooms: map[int]*room{1: {match: m}}}
	s.tickQuestTime(stat.started.Add(125 * time.Second))
	s.tickQuestTime(stat.started.Add(125 * time.Second))
	p, _ := db.QuestProgress(1, 2, 1)
	if p.Units != 2 {
		t.Fatal("minute tick duplicated", p)
	}
}

func TestEventQuestRejectsUnsupportedConditions(t *testing.T) {
	for _, condition := range []string{`{"EventType":99}`, `{"EventType":2,"UnknownFilter":true}`, `{"EventType":2,"Bot":2}`} {
		dir := t.TempDir()
		raw := `{"Version":1,"1":{"Active":true,"Tasks":{"1":{"Goal":1,"Conditions":{"1":` + condition + `}}}}}`
		if err := os.WriteFile(filepath.Join(dir, "EventQuests.json"), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadEventQuests(dir); err == nil {
			t.Fatal("unsupported filter silently broadened", condition)
		}
	}
	if err := validateQuestNotice("progress %d/%d (100%%)"); err != nil {
		t.Fatal(err)
	}
	if value := questNotice("progress %d/%d (100%%)", 5, 10); value != "progress 5/10 (100%)" {
		t.Fatal(value)
	}
	if err := validateQuestNotice("%s %d"); err == nil {
		t.Fatal("unsafe format accepted")
	}
}

func TestQuestOutboxRetriesPartialDelivery(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "outbox.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("outbox", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Outbox", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.GrantReward(1, store.Grant{Exp: 250}, 10, 0); err != nil {
		t.Fatal(err)
	}
	c := &client{user: store.User{ID: 1}, queue: make(chan outbound, 128)}
	s := &Server{Store: db, Log: slog.Default(), quests: []questTask{{QuestID: 1, TaskID: 1, Goal: 10, Conditions: []questCondition{{EventType: 5, GoalPoints: 1}}}, {QuestID: 1, TaskID: 2, Goal: 10, Conditions: []questCondition{{EventType: 5, GoalPoints: 1}}}}}
	if _, err = db.DB.Exec("CREATE TRIGGER failSecondTask BEFORE INSERT ON GoQuestTaskProgress WHEN NEW.taskID=2 BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.drainQuestEvents(c); err == nil {
		t.Fatal("partial delivery ignored database failure")
	}
	p, _ := db.QuestProgress(1, 1, 1)
	if p.Units != 2 {
		t.Fatal("level event not delivered", p)
	}
	events, _ := db.QuestEvents(1)
	if len(events) != 1 {
		t.Fatal("failed delivery acknowledged event", events)
	}
	if _, err = db.DB.Exec("DROP TRIGGER failSecondTask"); err != nil {
		t.Fatal(err)
	}
	if err = s.drainQuestEvents(c); err != nil {
		t.Fatal(err)
	}
	for _, task := range []int{1, 2} {
		p, _ = db.QuestProgress(1, 1, task)
		if p.Units != 2 {
			t.Fatal("partial retry duplicated level progress", task, p)
		}
	}
	events, _ = db.QuestEvents(1)
	if len(events) != 0 {
		t.Fatal("delivered event remains pending", events)
	}
}

func TestContinuousQuestCountsExactGoalAndResetsOnDeath(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "streak.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("streak", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Streak", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	c := &client{user: store.User{ID: 1}, playing: true, room: 1, queue: make(chan outbound, 128)}
	stat := &matchStat{user: c}
	m := &gameMatch{id: "streak", stats: map[int]*matchStat{1: stat}}
	s := &Server{Store: db, Log: slog.Default(), quests: []questTask{{QuestID: 1, TaskID: 1, Goal: 10, Conditions: []questCondition{{EventType: 2, GoalPoints: 1, Continuous: 2, KillerTeam: -1, VictimTeam: -1, VictimKillType: -1, GunID: -1, Bot: -1}}}}, rooms: map[int]*room{1: {id: 1, host: c, match: m}}}
	for i := 0; i < 3; i++ {
		if err = s.applyQuestEvent(c, m, stat, questEvent{Kind: 2}); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := db.QuestProgress(1, 1, 1)
	if p.Units != 1 {
		t.Fatal("two-kill streak counted at wrong threshold", p)
	}
	w := &protocol.Writer{}
	w.U8(3)
	w.U32(1)
	w.U16(12)
	w.U8(2)
	w.U32(1)
	w.U8(2)
	w.U16(0)
	w.Bytes(make([]byte, 24))
	if err = s.hostRequest(c, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	if err = s.applyQuestEvent(c, m, stat, questEvent{Kind: 2}); err != nil {
		t.Fatal(err)
	}
	p, _ = db.QuestProgress(1, 1, 1)
	if p.Units != 1 {
		t.Fatal("death did not reset streak", p)
	}
}
