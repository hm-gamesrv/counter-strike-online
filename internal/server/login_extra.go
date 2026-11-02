package server

import (
	"errors"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func (s *Server) sendExpiryNotices(c *client) error {
	items, err := s.Store.ExpiryNotices(c.user.ID)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	if len(items) > 255 {
		items = items[:255]
	}
	w := &protocol.Writer{}
	w.U8(62)
	w.U8(len(items))
	for _, item := range items {
		if item < 0 || item > 65535 {
			continue
		}
		w.U16(item)
		w.U8(0)
		w.U8(0)
	}
	c.send(67, w)
	return s.Store.ClearExpiryNotices(c.user.ID)
}

// grantCatalogReward grants one fixed catalog reward. Choice rewards stay
// pending because they need the client selection dialog.
func (s *Server) grantCatalogReward(c *client, rewardID int) error {
	p, err := s.buildCatalogReward(rewardID, 1)
	if err != nil {
		return err
	}
	if len(p.Choices) > 0 {
		return errors.New("choice reward needs the selection UI")
	}
	changed, err := s.Store.GrantReward(c.user.ID, p.Grant, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
	if err != nil {
		return err
	}
	if err = s.sendInventoryItems(c, changed); err != nil {
		return err
	}
	if w := rewardNotice(p.Label, p.NoticeItems, p.Grant); len(w.Data) <= protocol.MaxPayload {
		c.send(67, w)
	}
	return nil
}

func (s *Server) grantRewardNotices(c *client) error {
	rewards, err := s.Store.RewardNotices(c.user.ID)
	if err != nil {
		return err
	}
	for _, reward := range rewards {
		if err = s.grantCatalogReward(c, reward); err != nil {
			if s.Log != nil {
				s.Log.Warn("offline reward notice pending", "user", c.user.ID, "reward", reward, "error", err)
			}
			continue
		}
		if err = s.Store.DeleteRewardNotice(c.user.ID, reward); err != nil {
			return err
		}
	}
	return nil
}
