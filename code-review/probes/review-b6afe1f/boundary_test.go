package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func TestReviewExpiryDeletesDeliveredIdentities(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	if _, err := db.DB.Exec("WITH RECURSIVE n(x) AS (SELECT 260 UNION ALL SELECT x-1 FROM n WHERE x>1) INSERT INTO UserExpiryNotice(userID,itemID) SELECT 1,x FROM n"); err != nil {
		t.Fatal(err)
	}
	if err := s.sendExpiryNotices(c); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	r := &protocol.Reader{Data: msg.data}
	if r.U8() != 62 || r.U8() != 255 {
		t.Fatal("unexpected notice header")
	}
	delivered := map[int]bool{}
	for range 255 {
		delivered[r.U16()] = true
		r.U8()
		r.U8()
	}
	remaining, err := db.ExpiryNotices(1)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range remaining {
		if delivered[item] {
			t.Fatalf("already delivered notice %d remains; remaining=%v, want unsent IDs 256..260", item, remaining)
		}
	}
}

func TestReviewOfflineChoiceDoesNotAbortLogin(t *testing.T) {
	s, c, db := newLoginExtraServer(t)
	reward := s.rewards[3000]
	reward.Select = true
	s.rewards[3000] = reward
	if _, err := db.DB.Exec("INSERT INTO UserRewardNotice(userID,rewardID) VALUES(1,3000)"); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		err := s.grantRewardNotices(c)
		pending, queryErr := db.RewardNotices(1)
		if queryErr != nil {
			t.Fatal(queryErr)
		}
		if err != nil {
			t.Errorf("login attempt %d aborted: %v; pending=%v", attempt, err, pending)
		}
	}
}
