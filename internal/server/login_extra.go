package server

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// errRewardDeferred marks a notice that cannot be paid right now (choice UI,
// unknown definition, catalog gap). The row stays queued and the login continues.
var errRewardDeferred = errors.New("reward deferred")

func (s *Server) sendExpiryNotices(c *client) error {
	notices, err := s.Store.ExpiryNoticeRows(c.user.ID)
	if err != nil {
		return err
	}
	sendable := make([]int, 0, len(notices))
	delivered := make([]int, 0, len(notices))
	for _, notice := range notices {
		if notice.ItemID < 0 || notice.ItemID > 65535 {
			// Never representable on the wire; drop the row instead of retrying.
			delivered = append(delivered, notice.RowID)
			continue
		}
		if len(sendable) >= 255 {
			break
		}
		sendable = append(sendable, notice.ItemID)
		delivered = append(delivered, notice.RowID)
	}
	if len(sendable) == 0 {
		return s.Store.ClearExpiryNotices(c.user.ID, delivered)
	}
	w := &protocol.Writer{}
	w.U8(62)
	w.U8(len(sendable))
	for _, item := range sendable {
		w.U16(item)
		w.U8(0)
		w.U8(0)
	}
	c.send(67, w)
	return s.Store.ClearExpiryNotices(c.user.ID, delivered)
}

// grantCatalogReward grants one fixed catalog reward. Choice rewards stay
// pending because they need the client selection dialog.
func (s *Server) grantCatalogReward(c *client, rewardID int) error {
	p, err := s.buildCatalogReward(rewardID, 1)
	if err != nil {
		return err
	}
	if len(p.Choices) > 0 {
		return fmt.Errorf("%w: choice reward needs the selection UI", errRewardDeferred)
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
		if err = s.grantCatalogRewardNotice(c, reward); err != nil {
			if errors.Is(err, store.ErrRewardClaimed) {
				continue
			}
			if s.Log != nil {
				s.Log.Warn("offline reward notice pending", "user", c.user.ID, "reward", reward, "error", err)
			}
			if rewardRejection(err) {
				continue
			}
			return err
		}
	}
	return nil
}

// rewardRejection reports refusals that keep the notice queued and let the login
// continue, so one unpayable reward cannot lock the account out.
func rewardRejection(err error) bool {
	if errors.Is(err, store.ErrItemUnavailable) || errors.Is(err, store.ErrItemProtected) || errors.Is(err, store.ErrRewardLevel) || errors.Is(err, errRewardDeferred) {
		return true
	}
	return strings.Contains(err.Error(), "INVENTORY_FULL")
}

// grantCatalogRewardNotice consumes the notice row and grants its bundle in one
// transaction, so a failure between the two steps cannot pay it twice.
func (s *Server) grantCatalogRewardNotice(c *client, rewardID int) error {
	p, err := s.buildCatalogReward(rewardID, 1)
	if err != nil {
		return fmt.Errorf("%w: %v", errRewardDeferred, err)
	}
	if len(p.Choices) > 0 {
		return fmt.Errorf("%w: choice reward needs the selection UI", errRewardDeferred)
	}
	changed, err := s.Store.GrantRewardNotice(c.user.ID, rewardID, p.Grant, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
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
