package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestApplyEquipmentBonus(t *testing.T) {
	s := &Server{
		Config: config.Config{},
	}
	s.Config.GameMatch.CalcResults.BonusPercentage.Items = map[string]struct{ Exp, Points int }{
		"73": {
			Exp:    50,
			Points: 25,
		},
	}
	stat := &matchStat{
		exp:    100,
		points: 200,
		items: []store.Item{
			{
				ID:     73,
				InUse:  1,
				Status: 1,
			},
		},
	}
	s.applyEquipmentBonus(stat)
	if stat.itemExp != 50 || stat.itemPoints != 25 {
		t.Fatalf("item percentages exp=%d points=%d", stat.itemExp, stat.itemPoints)
	}
	if stat.bonusExp != 50 || stat.bonusPoints != 50 {
		t.Fatalf("bonus exp=%d points=%d", stat.bonusExp, stat.bonusPoints)
	}

	inactive := &matchStat{
		exp:    100,
		points: 200,
		items: []store.Item{
			{
				ID:    73,
				InUse: 0,
			},
		},
	}
	s.applyEquipmentBonus(inactive)
	if inactive.bonusExp != 0 || inactive.bonusPoints != 0 {
		t.Fatalf("inactive item applied bonus %d/%d", inactive.bonusExp, inactive.bonusPoints)
	}
}
func TestSortMatchStatsIndividualMode(t *testing.T) {
	first := &matchStat{
		user: &client{
			user: store.User{
				ID: 1,
			},
		},
		kills: 10,
		score: 1,
	}
	second := &matchStat{
		user: &client{
			user: store.User{
				ID: 2,
			},
		},
		kills: 5,
		score: 9,
	}
	stats := []*matchStat{first, second}
	sortMatchStats(stats, 4)
	if stats[0] != second {
		t.Fatal("individual mode did not rank by score")
	}
	sortMatchStats(stats, 0)
	if stats[0] != first {
		t.Fatal("round mode did not rank by kills")
	}
}
