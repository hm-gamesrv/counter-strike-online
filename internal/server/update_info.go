package server

import (
	"errors"
	"strings"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

const nicknameItemID = 65

func (s *Server) nicknameReply(c *client, code int) {
	text := ""
	switch code {
	case 2:
	case -1:
		text = "CSO_NICKNAME_TOO_SHORT"
	case -2:
		text = "CSO_NICKNAME_TOO_LONG"
	case -3:
		text = "CSO_NICKNAME_ALREADY_EXIST"
	case -4:
		text = "CSO_NICKNAME_INVALID_NAME_CHAR"
	case 8:
		text = "CSO_NEED_NICKNAME_ITEM"
	default:
		text = "CSO_NICKNAME_PROHIBITED"
	}
	w := &protocol.Writer{}
	w.U8(2)
	w.U8(code)
	w.String(text)
	c.send(69, w)
}

func (s *Server) broadcastUserInfo(c *client) {
	w := &protocol.Writer{}
	w.U32(c.user.ID)
	userInfo(w, c.character)
	c.send(157, w)
	for u := range s.clients {
		if u != c && (u.inLobby && c.inLobby || u.room != 0 && u.room == c.room) {
			u.send(157, w)
		}
	}
}

func (s *Server) renameRequest(c *client, r *protocol.Reader) error {
	name := r.CString()
	if r.Err != nil {
		return r.Err
	}
	if r.Pos != len(r.Data) {
		return errors.New("unexpected nickname request tail")
	}
	items, err := s.Store.Inventory(c.user.ID)
	if err != nil {
		return err
	}
	slot := 0
	for _, item := range items {
		if item.ID == nicknameItemID {
			slot = item.Slot
			break
		}
	}
	if slot == 0 {
		s.nicknameReply(c, 8)
		return nil
	}
	code, changed, err := s.Store.RenameCharacter(c.user.ID, slot, nicknameItemID, name)
	if err != nil {
		return err
	}
	s.nicknameReply(c, code)
	if code != 2 {
		return nil
	}
	c.character.Name = name
	if err = s.sendInventoryItems(c, changed); err != nil {
		return err
	}
	s.broadcastUserInfo(c)
	return nil
}

func (s *Server) updateInfoRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	switch kind {
	case 2:
		return s.renameRequest(c, r)
	case 4:
		nation, city, town := r.U16(), r.U16(), r.U16()
		if r.Err != nil {
			return r.Err
		}
		if r.Pos != len(r.Data) {
			return errors.New("unexpected location request tail")
		}
		if err := s.Store.SetLocation(c.user.ID, nation, city, town); err != nil {
			return err
		}
		c.character.Nation, c.character.City, c.character.Town = nation, city, town
		s.broadcastUserInfo(c)
		return nil
	case 7:
		r.U8()
		return r.Err
	case 15:
		color := r.U16()
		if r.Err != nil {
			return r.Err
		}
		if r.Pos != len(r.Data) {
			return errors.New("unexpected chat color request tail")
		}
		if color != 0 {
			definition, ok := s.items[color]
			if !ok || !strings.Contains(definition.Resource, "chatcolor_") {
				return nil
			}
		}
		if err := s.Store.SetChatColor(c.user.ID, color); err != nil {
			if errors.Is(err, store.ErrItemUnavailable) {
				c.notice("Item unavailable.")
				return nil
			}
			return err
		}
		c.character.ChatColorID = color
		s.broadcastUserInfo(c)
		return nil
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported update-info request", "kind", kind, "user", c.user.ID)
		}
	}
	return r.Err
}
