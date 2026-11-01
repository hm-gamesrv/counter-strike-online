package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestChoiceRewardRequiresOwnedPendingToken(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "choice.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("choice", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Choice", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Config: config.Config{InventorySlotMax: 10}, items: map[int]itemDefinition{1001: {ClassName: "RewardItem", Category: 9, RewardID: 1001}, 12: {Category: 3}, 14: {Category: 4}}, rewards: map[int]itemReward{1001: {Title: "Choose", Select: true, Items: map[string]struct{ Count, Duration int }{"12": {Count: 1}, "14": {Count: 1}}}}}
	c := &client{version: true, user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "Choice"}, queue: make(chan outbound, 128)}
	request := func(selection, token int, tail bool) error {
		w := &protocol.Writer{}
		w.U8(67)
		w.U32(selection)
		w.U32(token)
		if tail {
			w.U8(0)
		}
		return s.dispatch(c, protocol.Packet{ID: 67, Data: w.Data})
	}
	if err = request(1, 1001, false); err != nil {
		t.Fatal(err)
	}
	owned, _ := db.Inventory(1)
	if len(owned) != 1 || owned[0].ID != 1001 {
		t.Fatal("catalog ID granted free choice", owned)
	}
	for len(c.queue) > 0 {
		<-c.queue
	}
	if err = s.useRewardItem(c, owned[0], 1); err != nil {
		t.Fatal(err)
	}
	msg := <-c.queue
	p := &protocol.Reader{Data: msg.data}
	if msg.id != 67 || p.U8() != 67 {
		t.Fatal("missing selector")
	}
	p.U8()
	p.U16()
	if p.U8() != 2 {
		t.Fatal("incorrect option count")
	}
	for i := 1; i <= 2; i++ {
		if p.U8() != 1 || p.U32() != i {
			t.Fatal("incorrect choice identifier")
		}
		p.Bytes(14)
	}
	p.CString()
	p.CString()
	p.U8()
	token := p.U32()
	if p.Err != nil || p.Pos != len(p.Data) || token == 0 {
		t.Fatal("invalid selector token", p.Err)
	}
	if err = request(2, token, true); err == nil {
		t.Fatal("malformed selection accepted")
	}
	if err = request(3, token, false); err != nil {
		t.Fatal(err)
	}
	if err = request(0, token, false); err != nil {
		t.Fatal(err)
	}
	claims, err := db.PendingRewardClaims(1)
	if err != nil || len(claims) != 1 {
		t.Fatal("invalid/cancel lost pending reward", claims, err)
	}
	for len(c.queue) > 0 {
		<-c.queue
	}
	if err = s.resumeRewardClaims(c); err != nil {
		t.Fatal(err)
	}
	if msg = <-c.queue; msg.id != 67 || msg.data[0] != 67 {
		t.Fatal("pending selector not restored")
	}
	if err = request(2, token, false); err != nil {
		t.Fatal(err)
	}
	owned, _ = db.Inventory(1)
	if len(owned) != 1 || owned[0].ID != 14 {
		t.Fatal("selected option not awarded exclusively", owned)
	}
	if err = request(1, token, false); err != nil {
		t.Fatal(err)
	}
	owned, _ = db.Inventory(1)
	if len(owned) != 1 || owned[0].ID != 14 {
		t.Fatal("stale selection granted a second option", owned)
	}
}

func TestRandomRewardBinsAndPermanentDuration(t *testing.T) {
	raw := json.RawMessage(`[{"0":{"12":{"Count":1,"Duration":0}},"50":{"14":{"Count":2,"Duration":0}},"Durations":{"100":[3]}}]`)
	groups, err := compileRandomRewards(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		item, err := drawRandomReward(groups[0])
		if err != nil || item.ID != 14 || item.Count != 2 || item.Duration != 3 {
			t.Fatal("last-bin fallback or duration incorrect", item, err)
		}
	}
	groups[0].Items[1].Items[0].Duration = -1
	item, err := drawRandomReward(groups[0])
	if err != nil || item.Duration != -1 {
		t.Fatal("permanent marker replaced by random duration", item, err)
	}
	for _, raw := range []string{`[{"101":{"12":{}}}]`, `[{"0":{}}]`, `[{"Durations":{"100":[3]}}]`, `[{"100":{"bad":{}}}]`} {
		if _, err := compileRandomRewards(json.RawMessage(raw)); err == nil {
			t.Fatal("invalid random reward accepted", raw)
		}
	}
}

func TestRandomRewardRetryUsesFrozenOutcome(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "random.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Register("random", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Random", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	s := &Server{Store: db, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Config: config.Config{InventorySlotMax: 1}, items: map[int]itemDefinition{1001: {ClassName: "RewardItem", Category: 9, RewardID: 1001}, 12: {Category: 2}, 14: {Category: 2}, 15: {Category: 2}}, rewards: map[int]itemReward{1001: {Title: "Random", Items: map[string]struct{ Count, Duration int }{"14": {Count: 1}}, RandomItems: json.RawMessage(`[{"100":{"12":{"Count":1,"Duration":0}}}]`)}}}
	c := &client{user: store.User{ID: 1}, character: store.Character{ID: 1, Name: "Random"}, queue: make(chan outbound, 128)}
	items, _ := db.Inventory(1)
	if err = s.useRewardItem(c, items[0], 1); err != nil {
		t.Fatal(err)
	}
	claims, err := db.PendingRewardClaims(1)
	if err != nil || len(claims) != 1 {
		t.Fatal("failed grant lost random receipt", claims, err)
	}
	items, _ = db.Inventory(1)
	if len(items) != 1 || items[0].ID != 1001 {
		t.Fatal("capacity failure consumed source", items)
	}
	changed := s.rewards[1001]
	changed.RandomItems = json.RawMessage(`[{"100":{"15":{"Count":1}}}]`)
	s.rewards[1001] = changed
	if err = s.useRewardItem(c, items[0], 1); err != nil {
		t.Fatal(err)
	}
	retry, _ := db.PendingRewardClaims(1)
	if len(retry) != 1 || retry[0].Token != claims[0].Token || string(retry[0].Payload) != string(claims[0].Payload) {
		t.Fatal("retry changed frozen random result", retry)
	}
	s.Config.InventorySlotMax = 10
	if err = s.resumeRewardClaims(c); err != nil {
		t.Fatal(err)
	}
	items, _ = db.Inventory(1)
	if len(items) != 2 || items[0].ID != 14 || items[1].ID != 12 {
		t.Fatal("retry rerolled or awarded wrong bundle", items)
	}
	if err = s.finishRewardClaim(c, claims[0].Token, 0); err != nil {
		t.Fatal(err)
	}
	items, _ = db.Inventory(1)
	if len(items) != 2 {
		t.Fatal("frozen receipt paid twice", items)
	}
}
