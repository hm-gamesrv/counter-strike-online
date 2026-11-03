package server

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

type rewardChoice struct{ Notice, Grant store.Reward }
type pendingReward struct {
	Label       itemReward
	Grant       store.Grant
	NoticeItems []store.Reward
	Choices     []rewardChoice
}

func choiceRewardNotice(token int, p pendingReward) (*protocol.Writer, error) {
	if len(p.Choices) < 1 || len(p.Choices) > 255 {
		return nil, errors.New("invalid reward option count")
	}
	w := &protocol.Writer{}
	w.U8(67)
	w.U8(0)
	w.U16(0)
	w.U8(len(p.Choices))
	for i, option := range p.Choices {
		item := option.Notice
		w.U8(1)
		w.U32(i + 1)
		w.U16(item.ID)
		w.U16(item.Count)
		w.U16(item.Duration)
		w.U16(0)
		w.U32(0)
		w.U8(0)
		w.U8(0)
	}
	w.String(p.Label.Title)
	w.String(p.Label.Description)
	w.U8(0)
	w.U32(token)
	if len(w.Data) > protocol.MaxPayload {
		return nil, errors.New("reward choice frame too large")
	}
	return w, nil
}

func (s *Server) buildRewardClaim(target store.Item, count int) (pendingReward, error) {
	definition := s.items[target.ID]
	if definition.ClassName != "RewardItem" && definition.ClassName != "Requirement" {
		return pendingReward{}, errors.New("this consumable is not implemented yet")
	}
	p, err := s.buildCatalogReward(definition.RewardID, count)
	if err != nil {
		return p, err
	}
	p.Grant.SourceSlot, p.Grant.SourceItem, p.Grant.Consume, p.Grant.MinimumLevel = target.Slot, target.ID, count, definition.Level
	p.Grant.Protected = append(append([]int{}, s.Config.DefaultUser.DefaultItems...), s.Config.DefaultUser.PseudoDefaultItems...)
	return p, nil
}

func (s *Server) buildCatalogReward(rewardID, count int) (pendingReward, error) {
	var p pendingReward
	r, ok := s.rewards[rewardID]
	if !ok {
		return p, errors.New("reward definition is missing")
	}
	if count < 1 || count > 200 {
		return p, errors.New("reward item count must be 1..200")
	}
	if r.Select && count != 1 {
		return p, errors.New("open choice rewards one at a time")
	}
	p.Label = itemReward{Title: r.Title, Description: r.Description, Localized: r.Localized, Select: r.Select}
	groups := r.randomGroups
	var err error
	if groups == nil {
		groups, err = compileRandomRewards(r.RandomItems)
		if err != nil {
			return p, err
		}
	}
	if r.Select {
		groups = nil
	} // C++ choice rewards select a fixed option instead of rolling groups.
	keys := numericKeys(r.Items)
	perUse := len(groups)
	if !r.Select {
		perUse += len(keys)
	}
	base := rewardNotice(p.Label, nil, store.Grant{})
	if len(base.Data)+12 > protocol.MaxPayload || perUse > (protocol.MaxPayload-len(base.Data)-12)/15/count {
		return p, errors.New("reward bundle exceeds notice frame limit")
	}
	if r.Select {
		if len(keys) < 1 || len(keys) > 255 {
			return p, errors.New("invalid reward choice count")
		}
		for _, key := range keys {
			id, _ := strconv.Atoi(key)
			v := r.Items[key]
			raw := store.Reward{ID: id, Count: v.Count, Duration: v.Duration}
			items, err := s.prepareGrants([]store.Reward{raw})
			if err != nil {
				return p, err
			}
			p.Choices = append(p.Choices, rewardChoice{Notice: raw, Grant: items[0]})
		}
	}
	for repeat := 0; repeat < count; repeat++ {
		if !r.Select {
			for _, key := range keys {
				id, _ := strconv.Atoi(key)
				v := r.Items[key]
				p.NoticeItems = append(p.NoticeItems, store.Reward{ID: id, Count: v.Count, Duration: v.Duration})
			}
		}
		for _, group := range groups {
			item, err := drawRandomReward(group)
			if err != nil {
				return p, err
			}
			p.NoticeItems = append(p.NoticeItems, item)
		}
		for i, values := range [][]int{r.Points, r.Exp, r.HonorPoints} {
			v, err := randomRewardValue(values)
			if err != nil {
				return p, err
			}
			switch i {
			case 0:
				p.Grant.Points += v
			case 1:
				p.Grant.Exp += v
			case 2:
				p.Grant.HonorPoints += v
			}
		}
	}
	p.Grant.Items, err = s.prepareGrants(p.NoticeItems)
	if err != nil {
		return p, err
	}
	for _, v := range []int{p.Grant.Points, p.Grant.Exp, p.Grant.HonorPoints} {
		if uint64(v) > math.MaxUint32 {
			return p, errors.New("reward currency exceeds protocol range")
		}
	}
	if len(rewardNotice(p.Label, p.NoticeItems, p.Grant).Data)+15 > protocol.MaxPayload {
		return p, errors.New("reward notice too large")
	}
	return p, nil
}

func (s *Server) rewardError(c *client, err error) {
	if errors.Is(err, store.ErrItemUnavailable) || errors.Is(err, store.ErrItemProtected) || errors.Is(err, store.ErrRewardLevel) || errors.Is(err, store.ErrRewardClaimed) {
		c.notice(err.Error())
		return
	}
	s.Log.Warn("reward operation failed", "user", c.user.ID, "error", err)
	c.notice("Unable to grant reward. Check inventory space and pending rewards.")
}

func (s *Server) useRewardItem(c *client, target store.Item, count int) error {
	if count < 1 || count > 200 {
		return errors.New("reward item count must be 1..200")
	}
	if c.playing {
		c.notice("Use this item in the lobby or waiting room.")
		return nil
	}
	p, err := s.buildRewardClaim(target, count)
	if err != nil {
		c.notice(err.Error())
		return nil
	}
	if len(p.Choices) > 0 {
		if _, err = choiceRewardNotice(1, p); err != nil {
			return err
		}
	}
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	claim, err := s.Store.PrepareRewardClaim(c.user.ID, s.items[target.ID].RewardID, p.Grant, data)
	if err != nil {
		s.rewardError(c, err)
		return nil
	}
	return s.showRewardClaim(c, claim)
}

func (s *Server) showRewardClaim(c *client, claim store.RewardClaim) error {
	var p pendingReward
	if err := json.Unmarshal(claim.Payload, &p); err != nil {
		return err
	}
	if len(p.Choices) > 0 {
		w, err := choiceRewardNotice(claim.Token, p)
		if err != nil {
			return err
		}
		c.send(67, w)
		return nil
	}
	return s.finishRewardClaim(c, claim.Token, 0)
}

func (s *Server) finishRewardClaim(c *client, token, selection int) error {
	if c.playing {
		c.notice("Claim this reward in the lobby or waiting room.")
		return nil
	}
	claim, err := s.Store.PendingRewardClaim(c.user.ID, token)
	if err != nil {
		s.rewardError(c, err)
		return nil
	}
	var p pendingReward
	if err = json.Unmarshal(claim.Payload, &p); err != nil {
		return err
	}
	if len(p.Choices) > 0 {
		if selection < 1 || selection > len(p.Choices) {
			c.notice("Invalid reward selection.")
			return nil
		}
		selected := p.Choices[selection-1]
		p.Grant.Items = append(p.Grant.Items, selected.Grant)
		p.NoticeItems = append(p.NoticeItems, selected.Notice)
	} else if selection != 0 {
		c.notice("This reward has no selectable options.")
		return nil
	}
	p.Grant.ClaimToken = token
	notice := rewardNotice(p.Label, p.NoticeItems, p.Grant)
	if len(notice.Data) > protocol.MaxPayload {
		return errors.New("reward notice too large")
	}
	changed, err := s.Store.GrantReward(c.user.ID, p.Grant, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
	if err != nil {
		s.rewardError(c, err)
		return nil
	}
	equipped := s.autoEquipCostumes(c.user.ID, p.Grant.Items)
	if len(equipped) > 0 {
		// Auto-equip rewrote status and expiry after the grant snapshot was read,
		// so the packet has to come from the current rows.
		if changed, err = s.refreshItems(c.user.ID, changed); err != nil {
			return err
		}
	}
	if err = s.sendInventoryItems(c, changed); err != nil {
		return err
	}
	c.character, err = s.Store.Character(c.user.ID)
	if err != nil {
		return err
	}
	w := &protocol.Writer{}
	w.U32(c.user.ID)
	userInfo(w, c.character)
	c.send(157, w)
	c.send(67, notice)
	return s.drainQuestEvents(c)
}

func (s *Server) rewardSelection(c *client, p *protocol.Reader) error {
	if c.user.ID == 0 || c.character.Name == "" {
		return errors.New("authentication and character required")
	}
	selection, token := p.U32(), p.U32()
	if p.Err != nil {
		return p.Err
	}
	if p.Pos != len(p.Data) {
		return errors.New("unexpected reward selection tail")
	}
	if selection == 0 {
		return nil
	} // Closing the UI preserves the pending claim.
	return s.finishRewardClaim(c, token, selection)
}

func (s *Server) resumeRewardClaims(c *client) error {
	if c.playing {
		return nil
	}
	claims, err := s.Store.PendingRewardClaims(c.user.ID)
	if err != nil {
		return err
	}
	for _, claim := range claims {
		if err = s.showRewardClaim(c, claim); err != nil {
			return err
		}
	}
	return nil
}
