package store

import (
	"bytes"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func TestRewardClaimFrozenAcrossRestartAndClaimedOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "claims.db3")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	if err = s.Register("claims", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateCharacter(1, "Claims", config.DefaultUser{Level: 1, Points: 1000, PseudoDefaultItems: []int{12}}); err != nil {
		t.Fatal(err)
	}
	g := Grant{SourceSlot: 2, SourceItem: 1001, Consume: 1, Points: 100, Items: []Reward{{ID: 14, Count: 1}, {ID: 15, Count: 1}}}
	claim, err := s.PrepareRewardClaim(1, 1001, g, []byte(`{"roll":14}`))
	if err != nil {
		t.Fatal(err)
	}
	if claim.Token == 0 {
		t.Fatal("missing claim token")
	}
	retry, err := s.PrepareRewardClaim(1, 1001, g, []byte(`{"roll":999}`))
	if err != nil || retry.Token != claim.Token || !bytes.Equal(retry.Payload, claim.Payload) {
		t.Fatal("retry rerolled claim", retry, err)
	}
	if _, err = s.PendingRewardClaim(2, claim.Token); !errors.Is(err, ErrRewardClaimed) {
		t.Fatal("other account could access claim", err)
	}
	g.ClaimToken = claim.Token
	if _, err = s.GrantReward(1, g, 2, 0); err == nil {
		t.Fatal("full inventory accepted complete bundle")
	}
	if _, err = s.PendingRewardClaim(1, claim.Token); err != nil {
		t.Fatal("failed grant lost pending claim", err)
	}
	items, _ := s.Inventory(1)
	ch, _ := s.Character(1)
	if len(items) != 2 || items[1].ID != 1001 || ch.Points != 1000 {
		t.Fatal("failed claim changed source or balance", items, ch)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	retry, err = s.PendingRewardClaim(1, claim.Token)
	if err != nil || !bytes.Equal(retry.Payload, claim.Payload) {
		t.Fatal("restart lost frozen reward", retry, err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.GrantReward(1, g, 100, 0); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	ch, err = s.Character(1)
	if err != nil || success != 1 || ch.Points != 1100 {
		t.Fatal("claim paid more than once", success, ch, err)
	}
	if _, err = s.PendingRewardClaim(1, claim.Token); !errors.Is(err, ErrRewardClaimed) {
		t.Fatal("claimed reward still pending", err)
	}
	if _, err = s.GrantReward(1, g, 100, 0); !errors.Is(err, ErrRewardClaimed) {
		t.Fatal("replay accepted", err)
	}
}

func TestRewardClaimRetiresMissingOrExpiredSources(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "stale.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.Register("stale", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = s.CreateCharacter(1, "Stale", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	g := Grant{SourceSlot: 1, SourceItem: 1001, Consume: 1}
	first, err := s.PrepareRewardClaim(1, 1001, g, []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE UserInventory SET lockStatus=1 WHERE userID=1 AND slot=1"); err != nil {
		t.Fatal(err)
	}
	claims, err := s.PendingRewardClaims(1)
	if err != nil || len(claims) != 1 {
		t.Fatal("locked source lost pending reward", claims, err)
	}
	if _, err = s.DB.Exec("UPDATE UserInventory SET expiryDate=? WHERE userID=1 AND slot=1", nowMinutes()-1); err != nil {
		t.Fatal(err)
	}
	claims, err = s.PendingRewardClaims(1)
	if err != nil || len(claims) != 0 {
		t.Fatal("expired reward remains pending", claims, err)
	}
	if _, err = s.PendingRewardClaim(1, first.Token); !errors.Is(err, ErrRewardClaimed) {
		t.Fatal("expired token accepted", err)
	}
	if _, err = s.DB.Exec("UPDATE UserInventory SET expiryDate=0,lockStatus=0 WHERE userID=1 AND slot=1"); err != nil {
		t.Fatal(err)
	}
	second, err := s.PrepareRewardClaim(1, 1001, g, []byte("second"))
	if err != nil || second.Token == first.Token {
		t.Fatal("retired source blocked new claim", second, err)
	}
	if _, err = s.DB.Exec("DELETE FROM UserInventory WHERE userID=1 AND slot=1"); err != nil {
		t.Fatal(err)
	}
	claims, err = s.PendingRewardClaims(1)
	if err != nil || len(claims) != 0 {
		t.Fatal("missing source remains pending", claims, err)
	}
}
