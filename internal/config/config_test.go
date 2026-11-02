package config

import (
	"encoding/json"
	"testing"
)

func TestCommentsPreserveStrings(t *testing.T) {
	b := []byte("{\"url\":\"https://a/b//c\", /* block */ \"quote\":\"a\\\"//b\", // line\n\"port\":\"30002\"}")
	var v struct {
		URL, Quote string
		Port       string
	}
	if e := json.Unmarshal(StripComments(b), &v); e != nil {
		t.Fatal(e)
	}
	if v.URL != "https://a/b//c" || v.Quote != "a\"//b" || v.Port != "30002" {
		t.Fatal(v)
	}
}
func TestSurveyQuestionShapes(t *testing.T) {
	var checkbox SurveyQuestion
	if err := json.Unmarshal([]byte(`{"Question":"Q","AnswerType":0,"AnswerCheckBoxType":1,"Answers":{"2":{"Answer":"No"},"1":{"Answer":"Yes"}}}`), &checkbox); err != nil {
		t.Fatal(err)
	}
	if checkbox.AnswerType != 0 || checkbox.AnswerCheckBoxType != 1 || len(checkbox.Answers) != 2 {
		t.Fatalf("checkbox survey %+v", checkbox)
	}
	if checkbox.Answers[0].ID != 1 || checkbox.Answers[0].Answer != "Yes" || checkbox.Answers[1].ID != 2 {
		t.Fatalf("checkbox answers %+v", checkbox.Answers)
	}
	var text SurveyQuestion
	if err := json.Unmarshal([]byte(`{"Question":"Q","AnswerType":1,"Answers":{"Unk":7}}`), &text); err != nil {
		t.Fatal(err)
	}
	if text.TextEntryUnk != 7 || len(text.Answers) != 0 {
		t.Fatalf("text survey %+v", text)
	}
}
