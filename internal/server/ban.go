package server

import (
	"errors"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func (s *Server) sendBanLogin(c *client) error {
	names, err := s.Store.BanList(c.user.ID)
	if err != nil {
		return err
	}
	if len(names) != 0 {
		if len(names) > 65535 {
			return errors.New("ban list exceeds protocol range")
		}
		w := &protocol.Writer{}
		w.U8(0)
		w.U16(len(names))
		for _, name := range names {
			w.String(name)
		}
		if len(w.Data) > protocol.MaxPayload {
			return errors.New("ban list exceeds frame limit")
		}
		c.send(74, w)
	}
	settings, err := s.Store.BanSettings(c.user.ID)
	if err != nil {
		return err
	}
	w := &protocol.Writer{}
	w.U8(3)
	w.U8(settings)
	c.send(74, w)
	return nil
}

// chatAllowed applies the recipient's banSettings bit 2 to lobby, room and
// team chat. Database failures fail open so a transient read error cannot mute
// the whole server.
func (s *Server) chatAllowed(sender, recipient *client) bool {
	if sender == recipient {
		return true
	}
	settings, err := s.Store.BanSettings(recipient.user.ID)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("ban settings read failed", "user", recipient.user.ID, "error", err)
		}
		return true
	}
	if settings&2 == 0 {
		return true
	}
	banned, err := s.Store.IsBanned(recipient.user.ID, sender.character.Name)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("ban list read failed", "user", recipient.user.ID, "error", err)
		}
		return true
	}
	return !banned
}

func (s *Server) whisperDenied(sender, recipient *client) (string, bool) {
	settings, err := s.Store.BanSettings(sender.user.ID)
	if err != nil {
		return "", false
	}
	if settings&4 != 0 {
		return "MSG_TELL_SENDER_USING_BAN_CHAT_ALL", true
	}
	dest, err := s.Store.BanSettings(recipient.user.ID)
	if err != nil {
		return "", false
	}
	if dest&4 != 0 {
		return "MSG_TELL_LISTENER_USING_BAN_CHAT_ALL", true
	}
	if settings&2 != 0 {
		banned, err := s.Store.IsBanned(sender.user.ID, recipient.character.Name)
		if err == nil && banned {
			return "", true
		}
	}
	return "", false
}

func (s *Server) hostAllows(host, guest *client) (bool, error) {
	settings, err := s.Store.BanSettings(host.user.ID)
	if err != nil {
		return false, err
	}
	if settings&1 == 0 {
		return true, nil
	}
	banned, err := s.Store.IsBanned(host.user.ID, guest.character.Name)
	if err != nil {
		return false, err
	}
	return !banned, nil
}

func (s *Server) banRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	switch kind {
	case 0, 1:
		name := r.CString()
		if r.Err != nil {
			return r.Err
		}
		if r.Pos != len(r.Data) {
			return errors.New("unexpected ban request tail")
		}
		var code int
		var err error
		if kind == 0 {
			code, err = s.Store.AddBan(c.user.ID, name, s.Config.BanListMaxSize)
		} else {
			code, err = s.Store.RemoveBan(c.user.ID, name)
		}
		if err != nil {
			if s.Log != nil {
				s.Log.Warn("ban list update failed", "user", c.user.ID, "remove", kind == 1, "error", err)
			}
			c.notice("DB_QUERY_FAILED")
			return nil
		}
		switch code {
		case 1:
			w := &protocol.Writer{}
			w.U8(kind + 1)
			w.String(name)
			c.send(74, w)
		case -1:
			if kind == 0 {
				c.notice("BAN_ADD_FAIL_NICKNAME_NOT_EXIST")
			} else {
				c.notice("BAN_REMOVE_FAIL_NICKNAME_NOT_EXIST")
			}
		case -2:
			c.notice("BAN_ADD_FAIL_ID_EXIST")
		case -3:
			c.notice("BAN_ADD_FAIL_MAX_NUMBER_EXCESS")
		default:
			c.notice("DB_QUERY_FAILED")
		}
	case 2:
		settings := r.U8()
		if r.Err != nil {
			return r.Err
		}
		if r.Pos != len(r.Data) {
			return errors.New("unexpected ban settings tail")
		}
		if err := s.Store.SetBanSettings(c.user.ID, settings); err != nil {
			s.Log.Warn("ban settings update failed", "user", c.user.ID, "error", err)
			c.notice("DB_QUERY_FAILED")
			return nil
		}
		w := &protocol.Writer{}
		w.U8(3)
		w.U8(settings)
		c.send(74, w)
	case 4:
		if r.Pos != len(r.Data) {
			return errors.New("unexpected ban max-size tail")
		}
		w := &protocol.Writer{}
		w.U8(4)
		w.U16(s.Config.BanListMaxSize)
		c.send(74, w)
	default:
		s.Log.Debug("unsupported ban request", "kind", kind, "user", c.user.ID)
	}
	return r.Err
}
