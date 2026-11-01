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
