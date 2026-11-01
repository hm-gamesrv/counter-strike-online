package store

import (
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"testing"
)

func TestSettlementAtomicAndIdempotent(t *testing.T) {
	s := testStore(t)
	if e := s.Register("alice", "secret1", "127.0.0.1", 0); e != nil {
		t.Fatal(e)
	}
	if e := s.CreateCharacter(1, "Alice", config.DefaultUser{Level: 1, Points: 100}); e != nil {
		t.Fatal(e)
	}
	reward := MatchReward{UserID: 1, Exp: 250, Points: 200, Kills: 2, Deaths: 1, UpdateStats: true}
	if e := s.SettleMatch("round-1", []MatchReward{reward, {UserID: 99, Exp: 10, Points: 10}}); e == nil {
		t.Fatal("invalid participant accepted")
	}
	ch, e := s.Character(1)
	if e != nil || ch.Exp != 0 || ch.Points != 100 {
		t.Fatal("partial settlement", ch, e)
	}
	for i := 0; i < 2; i++ {
		if e := s.SettleMatch("round-1", []MatchReward{reward}); e != nil {
			t.Fatal(e)
		}
	}
	ch, e = s.Character(1)
	if e != nil || ch.Exp != 250 || ch.Level != 3 || ch.Points != 300 || ch.Kills != 2 || ch.Deaths != 1 {
		t.Fatal("settlement repeated or wrong", ch, e)
	}
}
