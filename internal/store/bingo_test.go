package store

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func TestBingoStateSlotsPrizesAndReset(t *testing.T) {
	s := testStore(t)
	if err := s.Register("bingo", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(1, "Bingo", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	slots := make([]BingoSlot, 25)
	for i := range slots {
		slots[i] = BingoSlot{Number: i}
	}
	prizes := make([]BingoPrize, 12)
	for i := range prizes {
		prizes[i] = BingoPrize{Index: i, ItemID: 567 + i%2, Count: 1, Duration: 30}
	}
	if err := s.InitBingo(1, slots, prizes); err != nil {
		t.Fatal(err)
	}
	state, err := s.BingoState(1)
	if err != nil || state.Status != 1 || state.CanPlay != 1 {
		t.Fatalf("state %+v, %v", state, err)
	}
	if got, err := s.BingoSlots(1); err != nil || len(got) != 25 || got[24].Number != 24 {
		t.Fatalf("slots %+v, %v", got, err)
	}
	if got, err := s.BingoPrizes(1); err != nil || len(got) != 12 || got[0].ItemID != 567 {
		t.Fatalf("prizes %+v, %v", got, err)
	}
	if err := s.OpenBingo(1, 7, []int{0, 5}); err != nil {
		t.Fatal(err)
	}
	slots, _ = s.BingoSlots(1)
	if slots[7].Opened != 1 {
		t.Fatal("number was not opened")
	}
	prizes, _ = s.BingoPrizes(1)
	if prizes[0].Opened != 1 || prizes[5].Opened != 1 || prizes[1].Opened != 0 {
		t.Fatalf("prize state %+v", prizes)
	}
	slots[0], slots[1] = slots[1], slots[0]
	if err = s.SetBingoSlots(1, slots); err != nil {
		t.Fatal(err)
	}
	slots, _ = s.BingoSlots(1)
	if slots[0].Number != 1 || slots[1].Number != 0 {
		t.Fatalf("shuffle slots %+v", slots)
	}
	if err = s.ResetBingo(1); err != nil {
		t.Fatal(err)
	}
	if _, err = s.BingoState(1); err == nil {
		t.Fatal("state remained after reset")
	}
}
