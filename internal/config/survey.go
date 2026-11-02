package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// SurveyAnswer is one selectable checkbox answer. IDs are one byte on the
// wire, so callers validate the range before serializing.
type SurveyAnswer struct {
	ID     int
	Answer string
}

// SurveyQuestion keeps the answer shape normalized so the server does not have
// to decode the mixed upstream representation (objects for checkboxes, an
// "Unk" number for text entry questions).
type SurveyQuestion struct {
	Question           string
	AnswerType         int
	AnswerCheckBoxType int
	TextEntryUnk       int
	Answers            []SurveyAnswer
}

func (q *SurveyQuestion) UnmarshalJSON(data []byte) error {
	var raw struct {
		Question           string
		AnswerType         int
		AnswerCheckBoxType int
		Answers            map[string]json.RawMessage
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	q.Question = raw.Question
	q.AnswerType = raw.AnswerType
	q.AnswerCheckBoxType = raw.AnswerCheckBoxType
	if raw.AnswerType == 1 {
		if value, ok := raw.Answers["Unk"]; ok {
			if err := json.Unmarshal(value, &q.TextEntryUnk); err != nil {
				return fmt.Errorf("survey text entry Unk: %w", err)
			}
		}
		return nil
	}
	for key, value := range raw.Answers {
		id, err := strconv.Atoi(key)
		if err != nil {
			return fmt.Errorf("survey answer id %q", key)
		}
		var entry struct {
			Answer string
		}
		if err := json.Unmarshal(value, &entry); err != nil {
			return fmt.Errorf("survey answer %q: %w", key, err)
		}
		q.Answers = append(q.Answers, SurveyAnswer{ID: id, Answer: entry.Answer})
	}
	sort.Slice(q.Answers, func(i, j int) bool { return q.Answers[i].ID < q.Answers[j].ID })
	return nil
}

type Survey struct {
	Title     string
	Questions map[string]SurveyQuestion
}
