package server

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

// gmCommand runs a GM chat command. It reports whether the command was
// recognized; unrecognized commands fall through to normal chat.
func (s *Server) gmCommand(c *client, text string, args []string) (bool, error) {
	switch args[0] {
	case "/help":
		c.chatNotice("Available commands: /help, /version, /disconnect, /getfreeslots, /status, /getuid")
		c.chatNotice("/addpoints, /givepoints, /sendnotice, /sendgmnotice")
		c.chatNotice("/ban, /hban, /ipban, /unban")
	case "/version":
		c.chatNotice("CSNZ Go server; client build 30.12.24")
	case "/disconnect":
		c.conn.Close()
	case "/status":
		c.notice(fmt.Sprintf("%s: %d/%d players, %d rooms, up %s", s.Config.HostName, len(s.users), s.Config.MaxPlayers, len(s.rooms), time.Since(s.started).Round(time.Second)))
	case "/getfreeslots":
		items, err := s.Store.Inventory(c.user.ID)
		if err != nil {
			return true, err
		}
		free := s.Config.InventorySlotMax - len(items)
		if free <= 0 {
			c.chatNotice("You have no free slots.")
			return true, nil
		}
		c.chatNotice(fmt.Sprintf("You have %d free slots.", free))
	case "/getuid":
		if len(args) < 2 {
			c.chatNotice("/getuid usage: /getuid <gameName>")
			return true, nil
		}
		id, err := s.Store.UserIDByName(args[1])
		if errors.Is(err, sql.ErrNoRows) {
			c.chatNotice("User does not exist")
			return true, nil
		}
		if err != nil {
			return true, err
		}
		c.chatNotice(strconv.Itoa(id))
	case "/sendnotice":
		if len(args) < 2 {
			c.chatNotice("/sendnotice usage: /sendnotice <msg>")
			return true, nil
		}
		s.broadcastNoticeBox(strings.TrimPrefix(text, args[0]+" "))
	case "/sendgmnotice":
		if len(args) < 2 {
			c.chatNotice("/sendgmnotice usage: /sendgmnotice <msg>")
			return true, nil
		}
		w := &protocol.Writer{}
		w.U8(40)
		w.String(c.character.Name)
		w.String(strings.TrimPrefix(text, args[0]+" "))
		for _, u := range s.users {
			if s.chatAllowed(c, u) {
				u.send(67, w)
			}
		}
	case "/addpoints":
		delta, ok := atoiArg(args, 1)
		if !ok {
			c.chatNotice("/addpoints usage: /addpoints <points>")
			return true, nil
		}
		points, err := s.Store.AddPoints(c.user.ID, delta)
		if err != nil {
			return true, err
		}
		c.character.Points = points
		s.broadcastUserInfo(c)
	case "/givepoints":
		id, ok := atoiArg(args, 1)
		delta, okDelta := atoiArg(args, 2)
		if !ok || !okDelta {
			c.chatNotice("/givepoints usage: /givepoints <userID> <points>")
			return true, nil
		}
		if done, err := s.rejectMissingUser(c, id); done {
			return true, err
		}
		points, err := s.Store.AddPoints(id, delta)
		if err != nil {
			return true, err
		}
		if target := s.users[id]; target != nil {
			target.character.Points = points
			s.broadcastUserInfo(target)
		}
	case "/ban":
		id, okID := atoiArg(args, 1)
		banType, okType := atoiArg(args, 2)
		days, okDays := atoiArg(args, 4)
		if len(args) < 5 || !okID || !okType || !okDays {
			c.chatNotice("/ban usage: /ban <userID> <type> <reason> <term>. Ban user's account")
			return true, nil
		}
		if done, err := s.rejectMissingUser(c, id); done {
			return true, err
		}
		name := ""
		if character, err := s.Store.Character(id); err == nil {
			name = character.Name
		}
		if err := s.Store.SetUserBan(id, banType, args[3], days, int(time.Now().Unix()/60)); err != nil {
			return true, err
		}
		s.kick(id)
		if banType == 1 && name != "" {
			s.broadcastChatNotice(fmt.Sprintf("%s is banned. Reason: %s", name, args[3]))
		}
	case "/hban":
		id, ok := atoiArg(args, 1)
		if !ok {
			c.chatNotice("/hban usage: /hban <userID>. Ban user by HWID")
			return true, nil
		}
		if done, err := s.rejectMissingUser(c, id); done {
			return true, err
		}
		hwid, err := s.Store.UserLastHWID(id)
		if err != nil {
			return true, err
		}
		if len(hwid) == 0 {
			c.chatNotice("User has no HWID")
			return true, nil
		}
		if err = s.Store.AddHWIDBan(hwid); err != nil {
			return true, err
		}
		s.kick(id)
	case "/ipban":
		id, ok := atoiArg(args, 1)
		if !ok {
			c.chatNotice("/ipban usage: /ipban <userID>. Ban user by IP")
			return true, nil
		}
		if done, err := s.rejectMissingUser(c, id); done {
			return true, err
		}
		ip, err := s.Store.UserLastIP(id)
		if err != nil {
			return true, err
		}
		if ip == "" {
			c.chatNotice("User has no IP")
			return true, nil
		}
		if err = s.Store.AddIPBan(ip); err != nil {
			return true, err
		}
		s.kick(id)
	case "/unban":
		id, ok := atoiArg(args, 1)
		if !ok {
			c.chatNotice("/unban usage: /unban <userID>. Unban last IP, HWID and user account")
			return true, nil
		}
		if done, err := s.rejectMissingUser(c, id); done {
			return true, err
		}
		ip, err := s.Store.UserLastIP(id)
		if err != nil {
			return true, err
		}
		hwid, err := s.Store.UserLastHWID(id)
		if err != nil {
			return true, err
		}
		if err = s.Store.ClearUserBan(id); err != nil {
			return true, err
		}
		if ip != "" {
			if err = s.Store.RemoveIPBan(ip); err != nil {
				return true, err
			}
		}
		if len(hwid) != 0 {
			if err = s.Store.RemoveHWIDBan(hwid); err != nil {
				return true, err
			}
		}
	default:
		return false, nil
	}
	return true, nil
}

// rejectMissingUser sends the upstream "User does not exist" notice and tells
// the caller to stop the command.
func (s *Server) rejectMissingUser(c *client, id int) (bool, error) {
	exists, err := s.Store.UserExists(id)
	if err != nil {
		return true, err
	}
	if !exists {
		c.chatNotice("User does not exist")
		return true, nil
	}
	return false, nil
}

func (s *Server) kick(id int) {
	if target := s.users[id]; target != nil {
		target.conn.Close()
	}
}

func (s *Server) broadcastNoticeBox(text string) {
	for _, u := range s.users {
		u.notice(text)
	}
}

func (s *Server) broadcastChatNotice(text string) {
	for _, u := range s.users {
		u.chatNotice(text)
	}
}

func atoiArg(args []string, index int) (int, bool) {
	if index >= len(args) {
		return 0, false
	}
	value, err := strconv.Atoi(args[index])
	if err != nil {
		return 0, false
	}
	return value, true
}
