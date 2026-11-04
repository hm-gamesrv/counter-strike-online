package server

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

const noticeURL = "https://discord.com/invite/EvUAY6D"

// loginWelcomeText is the message box the reference shows on every login; the
// text is a literal in CUserManager::SendLoginPacket
// (src/manager/usermanager.cpp:618), not a configuration value.
const loginWelcomeText = "EN: Welcome to the CSN:S server! The project is non-commercial. Don't trust people trying to sell you a server.\nServer developer Discord: https://discord.gg/EvUAY6D \n"

type noticeDefinition struct {
	ID          int
	Type        int
	Name        string
	Description string
	StartDate   int
	EndDate     int
}

func loadNotices(cfg map[string]config.Notice) ([]noticeDefinition, error) {
	if len(cfg) == 0 {
		return nil, nil
	}
	keys := make([]int, 0, len(cfg))
	byID := map[int]config.Notice{}
	for key, notice := range cfg {
		id, err := strconv.Atoi(key)
		if err != nil || id < 1 || id > 55535 {
			return nil, fmt.Errorf("invalid notice id %q", key)
		}
		keys = append(keys, id)
		byID[id] = notice
	}
	sort.Ints(keys)
	notices := make([]noticeDefinition, 0, len(keys))
	for _, id := range keys {
		notice := byID[id]
		if notice.Type < 0 || notice.Type > 4 {
			return nil, fmt.Errorf("notice %d has invalid type", id)
		}
		if len(notice.Name) == 0 || len(notice.Name) > 1024 || len(notice.Description) > 4096 {
			return nil, fmt.Errorf("notice %d has invalid text", id)
		}
		if notice.StartDate < 0 || notice.EndDate < 0 {
			return nil, fmt.Errorf("notice %d has invalid dates", id)
		}
		notices = append(notices, noticeDefinition{
			ID:          id,
			Type:        notice.Type,
			Name:        notice.Name,
			Description: notice.Description,
			StartDate:   notice.StartDate,
			EndDate:     notice.EndDate,
		})
	}
	return notices, nil
}

func (s *Server) sendMainMenuSkin(c *client) {
	if s.Config.MainMenuSkinEvent <= 0 {
		return
	}
	w := &protocol.Writer{}
	w.U8(3)
	w.U32(s.Config.MainMenuSkinEvent)
	c.send(159, w)
}

func (s *Server) sendNotices(c *client) {
	for _, notice := range s.notices {
		w := &protocol.Writer{}
		w.U8(65)
		w.U16(notice.ID + 10000)
		w.U8(notice.Type)
		w.String(noticeURL)
		w.U32(notice.StartDate / 60)
		w.U32(notice.EndDate / 60)
		w.String(notice.Name)
		w.String(notice.Description)
		if len(w.Data) <= protocol.MaxPayload {
			c.send(67, w)
		}
	}
}
