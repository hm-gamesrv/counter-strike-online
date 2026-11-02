package store

type SurveyQuestionAnswer struct {
	QuestionID int
	Answers    []string
}

func (s *Store) SurveyAnswered(id, survey int) (bool, error) {
	var answered int
	err := s.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM UserSurveyAnswer WHERE userID=? AND surveyID=?)", id, survey).Scan(&answered)
	return answered != 0, err
}

// SaveSurvey stores one row per selected answer. All rows are written in a
// single transaction so a partially recorded survey cannot look complete.
func (s *Store) SaveSurvey(id, survey int, answers []SurveyQuestionAnswer) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, answer := range answers {
		for _, text := range answer.Answers {
			if _, err = tx.Exec("INSERT INTO UserSurveyAnswer(surveyID,questionID,userID,answer) VALUES(?,?,?,?)", survey, answer.QuestionID, id, text); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
