package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

type questCondition struct {
	EventType      int
	GoalPoints     int
	PlayerCount    int
	GameModes      []int
	Maps           []int
	KillerTeam     int
	VictimKillType int
	VictimTeam     int
	Continuous     int
	GunID          int
	Bot            int
	MonsterType    int
	CheckForGun    bool
}
type questTask struct {
	QuestID, TaskID, Goal, RewardID int
	Notice                          struct {
		Goal    int
		UserMsg string
	}
	Conditions []questCondition
}
type questEvent struct {
	Kind           int
	Count          int
	GunID          int
	KillerTeam     int
	VictimTeam     int
	VictimKillType int
	VictimID       int
	MonsterType    int
	Won            bool
	Key            string
	MatchContext   *questMatchContext
}
type questMatchContext struct{ Mode, Map, Players int }

func loadEventQuests(dir string) ([]questTask, error) {
	var root map[string]json.RawMessage
	if err := config.ReadJSON(filepath.Join(dir, "EventQuests.json"), &root); err != nil {
		return nil, err
	}
	var version int
	if err := json.Unmarshal(root["Version"], &version); err != nil || version != 1 {
		return nil, errors.New("unsupported EventQuests.json version")
	}
	var result []questTask
	for _, qkey := range numericKeys(root) {
		qid, _ := strconv.Atoi(qkey)
		if qid < 1 || strconv.Itoa(qid) != qkey {
			return nil, errors.New("invalid event quest ID")
		}
		var quest struct {
			Active bool
			Tasks  map[string]json.RawMessage
		}
		if err := strictQuestJSON(root[qkey], &quest); err != nil {
			return nil, err
		}
		if !quest.Active {
			continue
		}
		for _, tkey := range numericKeys(quest.Tasks) {
			tid, _ := strconv.Atoi(tkey)
			if tid < 1 || strconv.Itoa(tid) != tkey {
				return nil, errors.New("invalid event task ID")
			}
			var raw struct {
				Goal, RewardID int
				Notice         struct {
					Goal    int
					UserMsg string
				}
				Conditions map[string]json.RawMessage
			}
			raw.Goal = 1
			if err := strictQuestJSON(quest.Tasks[tkey], &raw); err != nil {
				return nil, err
			}
			if qid < 1 || tid < 1 || raw.Goal < 1 || raw.Goal > 1000000000 || raw.RewardID < 0 || raw.Notice.Goal < 0 || len(raw.Conditions) == 0 || len(raw.Conditions) > 128 || len(raw.Notice.UserMsg) > 1024 {
				return nil, errors.New("invalid event quest task")
			}
			task := questTask{QuestID: qid, TaskID: tid, Goal: raw.Goal, RewardID: raw.RewardID, Notice: raw.Notice}
			if raw.Notice.Goal > 0 {
				if err := validateQuestNotice(raw.Notice.UserMsg); err != nil {
					return nil, err
				}
			}
			for _, ckey := range numericKeys(raw.Conditions) {
				if v, _ := strconv.Atoi(ckey); v < 1 || strconv.Itoa(v) != ckey {
					return nil, errors.New("invalid quest condition ID")
				}
				cond := questCondition{GoalPoints: 1, KillerTeam: -1, VictimKillType: -1, VictimTeam: -1, GunID: -1, Bot: -1}
				if err := strictQuestJSON(raw.Conditions[ckey], &cond); err != nil {
					return nil, err
				}
				if !slices.Contains([]int{0, 1, 2, 3, 5, 6, 7, 8, 10, 11, 12}, cond.EventType) || cond.GoalPoints < 1 || cond.GoalPoints > 1000000000 || cond.PlayerCount < 0 || cond.PlayerCount > 32 || cond.Bot < -1 || cond.Bot > 1 || cond.Continuous < 0 || cond.Continuous > 1000000 || cond.GunID < -1 || cond.GunID > 65535 || cond.KillerTeam < -1 || cond.KillerTeam > 2 || cond.VictimTeam < -1 || cond.VictimTeam > 2 || cond.VictimKillType < -1 || cond.VictimKillType > 65535 || cond.MonsterType < 0 {
					return nil, errors.New("unsupported or invalid quest condition")
				}
				if cond.EventType != 2 && (cond.KillerTeam != -1 || cond.VictimTeam != -1 || cond.VictimKillType != -1 || cond.GunID != -1 || cond.Bot != -1 || cond.Continuous != 0 || cond.CheckForGun) || cond.EventType != 10 && cond.MonsterType != 0 || (cond.EventType == 3 || cond.EventType == 5) && (len(cond.GameModes) > 0 || len(cond.Maps) > 0 || cond.PlayerCount != 0) {
					return nil, errors.New("quest filter does not apply to its event type")
				}
				if len(cond.GameModes) > 256 || len(cond.Maps) > 65536 {
					return nil, errors.New("too many quest filters")
				}
				for _, v := range cond.GameModes {
					if v < 0 || v > 255 {
						return nil, errors.New("invalid quest game mode")
					}
				}
				for _, v := range cond.Maps {
					if v < 0 || v > 65535 {
						return nil, errors.New("invalid quest map")
					}
				}
				task.Conditions = append(task.Conditions, cond)
			}
			if len(task.Conditions) != len(raw.Conditions) {
				return nil, errors.New("invalid quest condition ID")
			}
			result = append(result, task)
			if len(result) > 128 {
				return nil, errors.New("too many event quest tasks")
			}
		}
		if len(numericKeys(quest.Tasks)) != len(quest.Tasks) {
			return nil, errors.New("invalid quest task ID")
		}
	}
	if len(numericKeys(root)) != len(root)-1 {
		return nil, errors.New("invalid event quest ID")
	}
	return result, nil
}

func strictQuestJSON(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(target)
}
func validateQuestNotice(text string) error {
	count := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '%' {
			continue
		}
		i++
		if i >= len(text) || text[i] != '%' && text[i] != 'd' {
			return errors.New("quest notice only supports %d and %%")
		}
		if text[i] == 'd' {
			count++
		}
	}
	if count != 2 {
		return errors.New("quest notice needs progress and goal placeholders")
	}
	return nil
}
func questNotice(text string, units, goal int) string {
	// Templates are validated before use; replace C's two integer placeholders.
	values := []int{units, goal}
	var out strings.Builder
	index := 0
	for i := 0; i < len(text); i++ {
		if text[i] != '%' {
			out.WriteByte(text[i])
			continue
		}
		i++
		if text[i] == '%' {
			out.WriteByte('%')
		} else {
			out.WriteString(strconv.Itoa(values[index]))
			index++
		}
	}
	return out.String()
}

func questConditionMatches(cond questCondition, e questEvent, m *gameMatch, stat *matchStat) bool {
	if cond.EventType != e.Kind {
		return false
	}
	if e.Kind == 3 || e.Kind == 5 {
		return len(cond.GameModes) == 0 && len(cond.Maps) == 0 && cond.PlayerCount == 0
	}
	context := e.MatchContext
	if context == nil {
		if m == nil || stat == nil || !stat.user.playing {
			return false
		}
		context = &questMatchContext{m.mode, m.mapID, len(m.stats)}
	}
	if len(cond.GameModes) > 0 && !slices.Contains(cond.GameModes, context.Mode) || len(cond.Maps) > 0 && !slices.Contains(cond.Maps, context.Map) || context.Players < cond.PlayerCount {
		return false
	}
	if e.Kind == 1 {
		return e.Won
	}
	if e.Kind == 10 {
		return cond.MonsterType == e.MonsterType
	}
	if e.Kind != 2 {
		return true
	}
	if cond.KillerTeam > 0 && cond.KillerTeam != e.KillerTeam || cond.VictimTeam > 0 && cond.VictimTeam != e.VictimTeam || cond.VictimTeam == 0 && e.KillerTeam == e.VictimTeam || cond.VictimKillType >= 0 && cond.VictimKillType != e.VictimKillType || cond.GunID >= 0 && cond.GunID != e.GunID {
		return false
	}
	if cond.Bot == 1 && e.VictimID != 0 || cond.Bot == 0 && e.VictimID == 0 {
		return false
	}
	if cond.CheckForGun {
		if !slices.ContainsFunc(stat.items, func(item store.Item) bool { return item.ID == e.GunID && item.InUse != 0 && item.Status != 0 }) {
			return false
		}
	}
	return true
}

func (s *Server) questPayload(task questTask) ([]byte, error) {
	if task.RewardID == 0 {
		return nil, nil
	}
	p, err := s.buildCatalogReward(task.RewardID, 1)
	if err != nil {
		return nil, err
	}
	if len(p.Choices) > 0 {
		if _, err = choiceRewardNotice(1, p); err != nil {
			return nil, err
		}
	}
	return json.Marshal(p)
}

// Only trusted server events reach here. Progress is cumulative and durable;
// granting is deferred until the lobby so game/result caches stay consistent.
func (s *Server) applyQuestEvent(c *client, m *gameMatch, stat *matchStat, e questEvent) error {
	if len(s.quests) == 0 {
		return nil
	}
	for _, task := range s.quests {
		old, err := s.Store.QuestProgress(c.user.ID, task.QuestID, task.TaskID)
		if err != nil {
			return err
		}
		if old.Token != 0 || old.Units >= task.Goal {
			continue
		}
		delta := 0
		for index, cond := range task.Conditions {
			if cond.EventType != e.Kind {
				continue
			}
			key := [3]int{task.QuestID, task.TaskID, index}
			if !questConditionMatches(cond, e, m, stat) {
				if stat != nil && cond.Continuous > 0 {
					delete(stat.questStreaks, key)
				}
				continue
			}
			if cond.Continuous > 0 && e.Kind == 2 {
				if stat.questStreaks == nil {
					stat.questStreaks = map[[3]int]int{}
				}
				// The reference compares "continuous <= counter" after incrementing,
				// so a 2-kill streak pays out on the third kill, not the second.
				stat.questStreaks[key]++
				if stat.questStreaks[key] <= cond.Continuous {
					continue
				}
				delete(stat.questStreaks, key)
			}
			points := cond.GoalPoints
			if e.Kind == 5 {
				points = min(e.Count, task.Goal)
			}
			delta += min(points, task.Goal-delta)
		}
		if delta == 0 {
			continue
		}
		var payload []byte
		if old.Units+delta >= task.Goal {
			payload, err = s.questPayload(task)
			if err != nil {
				s.Log.Warn("quest reward unavailable; completion will be retained", "quest", task.QuestID, "task", task.TaskID, "error", err)
			}
		}
		_, applied, err := s.Store.RecordQuestProgressApplied(c.user.ID, task.QuestID, task.TaskID, task.Goal, delta, task.RewardID, payload, e.Key)
		if err != nil {
			return err
		}
		// The reference notifies when the match-local progress lands exactly on a
		// multiple of the notice goal, not when it crosses a boundary; a 15-point
		// condition with a 30-point notice goal therefore speaks every other kill.
		// A retried event key changes nothing in the database, so it must not move
		// the match-local counter either.
		if applied && task.Notice.Goal > 0 {
			temp := questMatchUnits(stat, task, delta)
			if temp%task.Notice.Goal == 0 {
				c.chatNotice(questNotice(task.Notice.UserMsg, temp, task.Goal))
			}
		}
	}
	return nil
}

// questMatchUnits tracks the per-match progress the reference keeps in
// UserQuestTaskProgress::unitsDone. The stored counter spans matches, so the
// notice rule (a multiple of the notice goal) has to run on this one.
func questMatchUnits(stat *matchStat, task questTask, delta int) int {
	if stat == nil {
		return delta
	}
	if stat.questUnits == nil {
		stat.questUnits = map[[2]int]int{}
	}
	key := [2]int{task.QuestID, task.TaskID}
	stat.questUnits[key] += delta
	return stat.questUnits[key]
}

func (s *Server) resumeQuestRewards(c *client) error {
	if err := s.drainQuestEvents(c); err != nil {
		return err
	}
	for _, task := range s.quests {
		progress, err := s.Store.QuestProgress(c.user.ID, task.QuestID, task.TaskID)
		if err != nil {
			return err
		}
		if progress.Units < task.Goal || progress.Token != 0 || task.RewardID == 0 {
			continue
		}
		payload, err := s.questPayload(task)
		if err != nil {
			continue
		}
		if _, err = s.Store.RecordQuestProgress(c.user.ID, task.QuestID, task.TaskID, task.Goal, 0, task.RewardID, payload); err != nil {
			return err
		}
	}
	return s.resumeRewardClaims(c)
}

func (s *Server) drainQuestEvents(c *client) error {
	events, err := s.Store.QuestEvents(c.user.ID)
	if err != nil {
		return err
	}
	for _, event := range events {
		e := questEvent{Kind: event.Kind, Count: event.Count, Won: event.Won, Key: "outbox/" + strconv.Itoa(event.ID)}
		if event.Kind == 1 {
			e.MatchContext = &questMatchContext{event.Mode, event.Map, event.Players}
		}
		if err = s.applyQuestEvent(c, nil, nil, e); err != nil {
			return err
		}
		if err = s.Store.CompleteQuestEvent(c.user.ID, event.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) tickQuestTime(now time.Time) {
	for _, room := range s.rooms {
		m := room.match
		if m == nil {
			continue
		}
		for _, stat := range m.stats {
			if stat.started.IsZero() || !stat.user.playing {
				continue
			}
			minutes := int(now.Sub(stat.started) / time.Minute)
			for attempts := 0; stat.questMinutes < minutes && attempts < 60; attempts++ {
				if err := s.applyQuestEvent(stat.user, m, stat, questEvent{Kind: 0, Key: m.id + "/minute/" + strconv.Itoa(stat.questMinutes+1)}); err != nil {
					s.Log.Error("quest time update failed", "user", stat.user.user.ID, "error", err)
					break
				}
				stat.questMinutes++
			}
		}
	}
}

func (s *Server) logQuestWarnings() {
	for _, task := range s.quests {
		if task.RewardID != 0 {
			if _, ok := s.rewards[task.RewardID]; !ok {
				s.Log.Warn("event quest references missing reward; progress is retained, award waits for configuration", "quest", task.QuestID, "task", task.TaskID, "reward", task.RewardID)
			}
		}
	}
}
