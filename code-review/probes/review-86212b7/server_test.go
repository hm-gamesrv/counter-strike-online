package server

import "testing"

func TestReview862RepeatedQuestEventDoesNotRepeatNoticeProgress(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	c.playing = true
	task := questTask{
		QuestID: 862,
		TaskID:  1,
		Goal:    100,
		Conditions: []questCondition{{
			EventType:  0,
			GoalPoints: 1,
		}},
	}
	task.Notice.Goal = 2
	task.Notice.UserMsg = "%d/%d"
	s.quests = []questTask{task}
	stat := &matchStat{user: c}
	m := &gameMatch{
		id:    "review862",
		stats: map[int]*matchStat{1: stat},
	}
	event := questEvent{
		Kind: 0,
		Key:  "review862/minute/1",
	}
	for i := 0; i < 2; i++ {
		if err := s.applyQuestEvent(c, m, stat, event); err != nil {
			t.Fatal(err)
		}
	}
	p, err := db.QuestProgress(1, 862, 1)
	if err != nil || p.Units != 1 {
		t.Fatalf("persisted units=%d error=%v", p.Units, err)
	}
	if units := stat.questUnits[[2]int{862, 1}]; units != 1 || len(c.queue) != 0 {
		t.Fatalf("duplicate event changed local progress: units=%d notices=%d, persisted=1", units, len(c.queue))
	}
}

func TestReview862CatalogRewardRefreshesOnlineCurrency(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	initial, err := db.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	c.character = initial
	s.rewards[3000] = itemReward{
		Points: []int{50},
	}
	if err = s.grantCatalogReward(c, 3000); err != nil {
		t.Fatal(err)
	}
	persisted, err := db.Character(1)
	if err != nil || persisted.Points != initial.Points+50 {
		t.Fatalf("persisted points=%d error=%v", persisted.Points, err)
	}
	sentUserInfo := false
	for len(c.queue) > 0 {
		msg := <-c.queue
		sentUserInfo = sentUserInfo || msg.id == 157
	}
	if c.character.Points != persisted.Points || !sentUserInfo {
		t.Fatalf("reward currency: persisted=%d cache=%d sent157=%v", persisted.Points, c.character.Points, sentUserInfo)
	}
}
