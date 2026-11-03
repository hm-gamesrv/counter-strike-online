package server

import (
	"database/sql"
	"errors"
	"math/rand"
	"strings"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

const bingoEventFlag = 1 << 0

type bingoPrizeConfig struct {
	ItemID   int
	Count    int
	Duration int
}

type bingoDefinition struct {
	Active bool
	Prizes []bingoPrizeConfig
}

var bingoLines = [12][5]int{
	{0, 1, 2, 3, 4},
	{5, 6, 7, 8, 9},
	{10, 11, 12, 13, 14},
	{15, 16, 17, 18, 19},
	{20, 21, 22, 23, 24},
	{0, 5, 10, 15, 20},
	{1, 6, 11, 16, 21},
	{2, 7, 12, 17, 22},
	{3, 8, 13, 18, 23},
	{4, 9, 14, 19, 24},
	{0, 6, 12, 18, 24},
	{4, 8, 12, 16, 20},
}

func loadBingo(cfg config.MiniGameBingo) (bingoDefinition, error) {
	result := bingoDefinition{Active: cfg.Active}
	for _, item := range cfg.Items {
		if item.ID < 1 || item.ID > 65535 {
			return result, errors.New("bingo prize item outside uint16")
		}
		if item.Count < 1 || item.Count > 255 || item.Duration < 0 || item.Duration > 255 {
			return result, errors.New("bingo prize count/duration outside uint8")
		}
		result.Prizes = append(result.Prizes, bingoPrizeConfig{
			ItemID:   item.ID,
			Count:    item.Count,
			Duration: item.Duration,
		})
	}
	if result.Active && len(result.Prizes) < 12 {
		return result, errors.New("active bingo needs at least 12 prize items")
	}
	return result, nil
}

func (s *Server) bingoRequest(c *client, r *protocol.Reader) error {
	if !s.bingo.Active {
		c.notice("BINGO_EVENT_NOT_IN_PROGRESS")
		return r.Err
	}
	request := r.U8()
	switch request {
	case 0:
		return s.sendBingoUpdate(c)
	case 1:
		return s.bingoOpen(c)
	case 3:
		return s.bingoReset(c)
	case 4:
		return s.bingoShuffle(c)
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported bingo request", "request", request, "user", c.user.ID)
		}
	}
	return r.Err
}

func (s *Server) initBingo(c *client) error {
	numbers := rand.Perm(100)
	slots := make([]store.BingoSlot, 25)
	for i := range slots {
		slots[i] = store.BingoSlot{Number: numbers[i]}
	}
	prizes := append([]bingoPrizeConfig{}, s.bingo.Prizes...)
	rand.Shuffle(len(prizes), func(i, j int) { prizes[i], prizes[j] = prizes[j], prizes[i] })
	rows := make([]store.BingoPrize, 12)
	for i := range rows {
		rows[i] = store.BingoPrize{
			Index:    i,
			ItemID:   prizes[i].ItemID,
			Count:    prizes[i].Count,
			Duration: prizes[i].Duration,
		}
	}
	return s.Store.InitBingo(c.user.ID, slots, rows)
}

func (s *Server) sendBingoUpdate(c *client) error {
	state, err := s.Store.BingoState(c.user.ID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	slots, slotErr := s.Store.BingoSlots(c.user.ID)
	prizes, prizeErr := s.Store.BingoPrizes(c.user.ID)
	if errors.Is(err, sql.ErrNoRows) || slotErr != nil || prizeErr != nil || state.Status == 0 || len(slots) != 25 || len(prizes) != 12 {
		if slotErr != nil && !errors.Is(slotErr, sql.ErrNoRows) {
			return slotErr
		}
		if prizeErr != nil && !errors.Is(prizeErr, sql.ErrNoRows) {
			return prizeErr
		}
		if err = s.initBingo(c); err != nil {
			return err
		}
		if slots, err = s.Store.BingoSlots(c.user.ID); err != nil {
			return err
		}
		if prizes, err = s.Store.BingoPrizes(c.user.ID); err != nil {
			return err
		}
	}
	w := &protocol.Writer{}
	w.U8(0)
	w.U8(0)
	w.U8(len(slots))
	for _, slot := range slots {
		w.U8(slot.Number)
		w.U8(slot.Opened)
	}
	w.U8(len(prizes))
	for _, prize := range prizes {
		w.U16(prize.ItemID)
		w.U8(prize.Count)
		w.U8(prize.Duration)
		w.U8(prize.Opened)
	}
	c.send(89, w)
	return nil
}

func (s *Server) bingoOpen(c *client) error {
	if err := s.sendBingoUpdate(c); err != nil {
		return err
	}
	slots, err := s.Store.BingoSlots(c.user.ID)
	if err != nil {
		return err
	}
	prizes, err := s.Store.BingoPrizes(c.user.ID)
	if err != nil {
		return err
	}
	var candidates []int
	for i, slot := range slots {
		if slot.Opened == 0 {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	selected := candidates[rand.Intn(len(candidates))]
	slots[selected].Opened = 1
	var completed []int
	for index, line := range bingoLines {
		if index >= len(prizes) || prizes[index].Opened != 0 {
			continue
		}
		done := true
		for _, position := range line {
			if slots[position].Opened == 0 {
				done = false
				break
			}
		}
		if done {
			completed = append(completed, index)
		}
	}
	var rewards []store.Reward
	prizeItems := make([][]store.Reward, len(completed))
	for i, index := range completed {
		prize := prizes[index]
		items, e := s.prepareGrants([]store.Reward{{
			ID:       prize.ItemID,
			Count:    prize.Count,
			Duration: prize.Duration,
		}})
		if e != nil {
			return e
		}
		prizeItems[i] = items
		rewards = append(rewards, items...)
	}
	// One transaction covers the number, every completed line and the prizes, so
	// a retry after "inventory full" cannot duplicate or lose a reward.
	items, err := s.Store.OpenBingoAwards(c.user.ID, slots[selected].Number, completed, rewards, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
	if err != nil {
		if strings.Contains(err.Error(), "INVENTORY_FULL") {
			c.notice("Inventory is full.")
			return nil
		}
		return err
	}
	if len(rewards) > 0 {
		if err = s.sendInventoryItems(c, items); err != nil {
			return err
		}
		for _, items := range prizeItems {
			notice := rewardNotice(itemReward{
				Title:       "Bingo Event",
				Description: "Congratulations! You have completed a bingo line.",
			}, items, store.Grant{})
			if len(notice.Data) <= protocol.MaxPayload {
				c.send(67, notice)
			}
		}
	}
	return s.sendBingoUpdate(c)
}

func (s *Server) bingoShuffle(c *client) error {
	slots, err := s.Store.BingoSlots(c.user.ID)
	if err != nil {
		return err
	}
	prizes, err := s.Store.BingoPrizes(c.user.ID)
	if err != nil {
		return err
	}
	openedSlots, openedPrizes := 0, 0
	for _, slot := range slots {
		if slot.Opened != 0 {
			openedSlots++
		}
	}
	for _, prize := range prizes {
		if prize.Opened != 0 {
			openedPrizes++
		}
	}
	if openedSlots > 11 || openedPrizes > 1 {
		c.notice("BINGO_SHUFFLE_NUMBERS_FAILED_SHUFFLE_LIMIT_EXCEED")
		return nil
	}
	// Shuffle whole slots, not just the numbers: opened marks belong to the number
	// that was opened, so moving numbers alone would hand the player a different
	// set of opened numbers than the one they drew.
	rand.Shuffle(len(slots), func(i, j int) { slots[i], slots[j] = slots[j], slots[i] })
	if err = s.Store.SetBingoSlots(c.user.ID, slots); err != nil {
		return err
	}
	return s.sendBingoUpdate(c)
}

func (s *Server) bingoReset(c *client) error {
	if err := s.Store.ResetBingo(c.user.ID); err != nil {
		return err
	}
	if err := s.initBingo(c); err != nil {
		return err
	}
	return s.sendBingoUpdate(c)
}
