package server

import (
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

type surveyAnswer struct {
	ID     int
	Answer string
}
type surveyQuestion struct {
	ID                 int
	Question           string
	AnswerType         int
	AnswerCheckBoxType int
	TextEntryUnk       int
	Answers            []surveyAnswer
}
type surveyDefinition struct {
	ID        int
	Title     string
	Questions []surveyQuestion
}

const (
	surveyAnswerOK      = 0
	surveyAnswerInvalid = 1
	surveyAnswerDBError = 2
)

func loadSurveys(cfg map[string]config.Survey) ([]surveyDefinition, error) {
	if len(cfg) == 0 {
		return nil, nil
	}
	keys := make([]int, 0, len(cfg))
	byID := map[int]config.Survey{}
	for key, survey := range cfg {
		id, err := strconv.Atoi(key)
		if err != nil || id < 1 || id > 65535 {
			return nil, fmt.Errorf("invalid survey id %q", key)
		}
		keys = append(keys, id)
		byID[id] = survey
	}
	sort.Ints(keys)
	surveys := make([]surveyDefinition, 0, len(keys))
	for _, id := range keys {
		survey := byID[id]
		if len(survey.Title) == 0 || len(survey.Title) > 1024 {
			return nil, fmt.Errorf("survey %d title must be 1..1024 bytes", id)
		}
		if len(survey.Questions) == 0 || len(survey.Questions) > 255 {
			return nil, fmt.Errorf("survey %d must have 1..255 questions", id)
		}
		definition := surveyDefinition{ID: id, Title: survey.Title}
		questionKeys := make([]int, 0, len(survey.Questions))
		questionByID := map[int]config.SurveyQuestion{}
		for key, question := range survey.Questions {
			questionID, err := strconv.Atoi(key)
			if err != nil || questionID < 1 || questionID > 255 {
				return nil, fmt.Errorf("survey %d has invalid question id %q", id, key)
			}
			questionKeys = append(questionKeys, questionID)
			questionByID[questionID] = question
		}
		sort.Ints(questionKeys)
		for _, questionID := range questionKeys {
			question := questionByID[questionID]
			if len(question.Question) == 0 || len(question.Question) > 1024 {
				return nil, fmt.Errorf("survey %d question %d text must be 1..1024 bytes", id, questionID)
			}
			if question.AnswerType < 0 || question.AnswerType > 2 {
				return nil, fmt.Errorf("survey %d question %d has invalid answer type", id, questionID)
			}
			value := surveyQuestion{ID: questionID, Question: question.Question, AnswerType: question.AnswerType}
			if question.AnswerType == 1 {
				if question.TextEntryUnk < 0 || question.TextEntryUnk > 65535 {
					return nil, fmt.Errorf("survey %d question %d has invalid text entry value", id, questionID)
				}
				value.TextEntryUnk = question.TextEntryUnk
			} else {
				if question.AnswerCheckBoxType < 0 || question.AnswerCheckBoxType > 2 {
					return nil, fmt.Errorf("survey %d question %d has invalid checkbox type", id, questionID)
				}
				if len(question.Answers) == 0 || len(question.Answers) > 255 {
					return nil, fmt.Errorf("survey %d question %d must have 1..255 answers", id, questionID)
				}
				value.AnswerCheckBoxType = question.AnswerCheckBoxType
				for _, answer := range question.Answers {
					if answer.ID < 1 || answer.ID > 255 || len(answer.Answer) == 0 || len(answer.Answer) > 255 {
						return nil, fmt.Errorf("survey %d question %d has invalid answer %d", id, questionID, answer.ID)
					}
					value.Answers = append(value.Answers, surveyAnswer{ID: answer.ID, Answer: answer.Answer})
				}
			}
			definition.Questions = append(definition.Questions, value)
		}
		surveys = append(surveys, definition)
	}
	return surveys, nil
}

func (s *Server) sendSurveys(c *client) error {
	for _, survey := range s.surveys {
		answered, err := s.Store.SurveyAnswered(c.user.ID, survey.ID)
		if err != nil {
			return err
		}
		if answered {
			continue
		}
		w := &protocol.Writer{}
		w.U8(0)
		w.U32(survey.ID)
		w.String(survey.Title)
		w.U8(len(survey.Questions))
		for _, question := range survey.Questions {
			w.U8(question.ID)
			w.String(question.Question)
			w.U8(question.AnswerType)
			if question.AnswerType == 1 {
				w.U16(question.TextEntryUnk)
				continue
			}
			w.U8(question.AnswerCheckBoxType)
			w.U8(len(question.Answers))
			for _, answer := range question.Answers {
				w.U8(answer.ID)
				w.String(answer.Answer)
			}
		}
		c.send(87, w)
	}
	return nil
}

func (s *Server) survey(id int) (surveyDefinition, bool) {
	for _, survey := range s.surveys {
		if survey.ID == id {
			return survey, true
		}
	}
	return surveyDefinition{}, false
}

func (q surveyQuestion) answer(id int) (surveyAnswer, bool) {
	for _, answer := range q.Answers {
		if answer.ID == id {
			return answer, true
		}
	}
	return surveyAnswer{}, false
}

func (s *Server) surveyReply(c *client, result int) {
	w := &protocol.Writer{}
	w.U8(11)
	w.U8(result)
	c.send(87, w)
}

func (s *Server) surveyRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	switch kind {
	case 11:
		surveyID := r.U32()
		if r.Err != nil {
			return r.Err
		}
		survey, ok := s.survey(surveyID)
		if !ok {
			s.surveyReply(c, surveyAnswerInvalid)
			return nil
		}
		count := r.U8()
		if r.Err != nil {
			return r.Err
		}
		if count == 0 || count > len(survey.Questions) {
			s.surveyReply(c, surveyAnswerInvalid)
			return nil
		}
		answers := make([]store.SurveyQuestionAnswer, 0, count)
		for i := 0; i < count; i++ {
			questionID := r.U8()
			var question surveyQuestion
			found := false
			for _, candidate := range survey.Questions {
				if candidate.ID == questionID {
					question, found = candidate, true
					break
				}
			}
			if !found {
				s.surveyReply(c, surveyAnswerInvalid)
				return nil
			}
			answerType := r.U8()
			if answerType == 1 || answerType == 2 {
				text := r.CString()
				if r.Err != nil {
					return r.Err
				}
				if len(text) == 0 || len(text) > 255 {
					s.surveyReply(c, surveyAnswerInvalid)
					return nil
				}
				answers = append(answers, store.SurveyQuestionAnswer{QuestionID: questionID, Answers: []string{text}})
				continue
			}
			selected := r.U8()
			if r.Err != nil {
				return r.Err
			}
			if selected == 0 || selected > len(question.Answers) {
				s.surveyReply(c, surveyAnswerInvalid)
				return nil
			}
			var texts []string
			seen := map[int]bool{}
			for j := 0; j < selected; j++ {
				answerID := r.U8()
				if r.Err != nil {
					return r.Err
				}
				answer, ok := question.answer(answerID)
				if !ok || seen[answerID] {
					s.surveyReply(c, surveyAnswerInvalid)
					return nil
				}
				seen[answerID] = true
				texts = append(texts, answer.Answer)
			}
			answers = append(answers, store.SurveyQuestionAnswer{QuestionID: questionID, Answers: texts})
		}
		if r.Err != nil {
			return r.Err
		}
		if r.Pos != len(r.Data) {
			return errors.New("unexpected survey request tail")
		}
		if err := s.Store.SaveSurvey(c.user.ID, surveyID, answers); err != nil {
			if s.Log != nil {
				s.Log.Error("survey answer failed", "user", c.user.ID, "survey", surveyID, "error", err)
			}
			s.surveyReply(c, surveyAnswerDBError)
			return nil
		}
		s.surveyReply(c, surveyAnswerOK)
	case 12:
		r.U8()
		r.CString()
		return r.Err
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported survey request", "kind", kind, "user", c.user.ID)
		}
		return r.Err
	}
	return nil
}
