package server

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

// gmCommand runs a GM chat command. It reports whether the command was
// recognized; unrecognized commands fall through to normal chat.
func (s *Server) gmCommand(c *client, text string, args []string) (bool, error) {
	switch args[0] {
	case "/help":
		c.chatNotice("Available commands: /help, /version, /disconnect, /getfreeslots, /status, /getuid")
		c.chatNotice("/sendnotice, /sendgmnotice, /addpoints, /givepoints, /addexp")
		c.chatNotice("/additem, /giveitem, /removeitem, /addallitems, /setitemstatus, /setiteminuse")
		c.chatNotice("/givereward, /giverewardtoall")
		c.chatNotice("/addwpnreleasechar, /ban, /hban, /ipban, /unban, /tournament")
		c.chatNotice("/addgroom, /reqsnapshot")
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
	case "/additem":
		itemID, ok := atoiArg(args, 1)
		if !ok {
			c.chatNotice("/additem usage: /additem <itemID> <count> <duration>")
			return true, nil
		}
		if _, known := s.items[itemID]; !known {
			c.chatNotice("Item ID you wrote does not exist in the item database")
			return true, nil
		}
		count, duration := gmCountDuration(args, 2, 3)
		changed, err := s.gmGrantItem(c.user.ID, itemID, count, duration)
		if err != nil {
			return true, err
		}
		if err = s.sendInventoryItems(c, changed); err != nil {
			return true, err
		}
		c.chatNotice(fmt.Sprintf("Item %d x%d added.", itemID, count))
	case "/giveitem":
		id, okID := atoiArg(args, 1)
		itemID, okItem := atoiArg(args, 2)
		if !okID || !okItem {
			c.chatNotice("/giveitem usage: /giveitem <userID> <itemID> <count> <duration>")
			return true, nil
		}
		if _, known := s.items[itemID]; !known {
			c.chatNotice("Item ID you wrote does not exist in the item database")
			return true, nil
		}
		if done, err := s.rejectMissingUser(c, id); done {
			return true, err
		}
		count, duration := gmCountDuration(args, 3, 4)
		changed, err := s.gmGrantItem(id, itemID, count, duration)
		if err != nil {
			return true, err
		}
		if target := s.users[id]; target != nil {
			if err = s.sendInventoryItems(target, changed); err != nil {
				return true, err
			}
			target.chatNotice(fmt.Sprintf("GM gave you item %d x%d.", itemID, count))
		}
	case "/removeitem":
		itemID, ok := atoiArg(args, 1)
		if !ok {
			c.chatNotice("/removeitem usage: /removeitem <itemID> <slot>")
			return true, nil
		}
		items, err := s.Store.Inventory(c.user.ID)
		if err != nil {
			return true, err
		}
		slots := make([]int, 0, len(items))
		for _, item := range items {
			if item.ID == itemID && item.Count > 0 {
				slots = append(slots, item.Slot)
			}
		}
		if len(slots) == 0 {
			c.chatNotice("Item does not exist")
			return true, nil
		}
		index, hasSlot := atoiArg(args, 2)
		if !hasSlot {
			c.chatNotice("Select one of the available item slots to remove:")
			for i, slot := range slots {
				c.chatNotice(fmt.Sprintf("%d) slot %d", i, slot))
			}
			return true, nil
		}
		if index < 0 || index >= len(slots) {
			c.chatNotice(fmt.Sprintf("You have written an invalid slot ID %d", index))
			return true, nil
		}
		if slices.Contains(s.Config.DefaultUser.DefaultItems, itemID) || slices.Contains(s.Config.DefaultUser.PseudoDefaultItems, itemID) {
			c.chatNotice(fmt.Sprintf("Item (%d, %d) cannot be removed", itemID, index))
			return true, nil
		}
		changed, err := s.Store.DeleteItems(c.user.ID, []int{slots[index]}, nil)
		if err != nil {
			return true, err
		}
		// Removal may clear a cosmetic selection in the database; refresh the
		// cached character before the next user-info broadcast.
		s.refreshCharacterCache(c)
		if err = s.sendInventoryItems(c, changed); err != nil {
			return true, err
		}
		c.chatNotice(fmt.Sprintf("Item (%d, %d) removed", itemID, index))
	case "/addexp":
		exp, ok := atoiArg(args, 1)
		if !ok || exp < 0 {
			c.chatNotice("/addexp usage: /addexp <exp>")
			return true, nil
		}
		if _, err := s.Store.GrantReward(c.user.ID, store.Grant{Exp: exp}, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems)); err != nil {
			return true, err
		}
		character, err := s.Store.Character(c.user.ID)
		if err != nil {
			return true, err
		}
		c.character = character
		s.broadcastUserInfo(c)
	case "/givereward":
		id, okID := atoiArg(args, 1)
		rewardID, okReward := atoiArg(args, 2)
		if !okID || !okReward {
			c.chatNotice("/givereward usage: /givereward <userID> <rewardID>")
			return true, nil
		}
		if done, err := s.rejectMissingUser(c, id); done {
			return true, err
		}
		p, err := s.buildCatalogReward(rewardID, 1)
		if err != nil {
			c.chatNotice("Reward does not exist")
			return true, nil
		}
		if len(p.Choices) > 0 {
			c.chatNotice("Choice reward needs the selection UI")
			return true, nil
		}
		if err = s.gmGrantReward(id, p); err != nil {
			return true, err
		}
	case "/giverewardtoall":
		rewardID, ok := atoiArg(args, 1)
		if !ok {
			c.chatNotice("/giverewardtoall usage: /giverewardtoall <rewardID>")
			return true, nil
		}
		p, err := s.buildCatalogReward(rewardID, 1)
		if err != nil {
			c.chatNotice("Reward does not exist")
			return true, nil
		}
		if len(p.Choices) > 0 {
			c.chatNotice("Choice reward needs the selection UI")
			return true, nil
		}
		ids, err := s.Store.UserIDs()
		if err != nil {
			return true, err
		}
		sent := 0
		for _, id := range ids {
			if err = s.gmGrantReward(id, p); err != nil {
				if s.Log != nil {
					s.Log.Warn("GM reward failed", "user", id, "reward", rewardID, "error", err)
				}
				continue
			}
			sent++
		}
		c.chatNotice(fmt.Sprintf("Reward %d sent to %d users.", rewardID, sent))
	case "/tournament":
		next := 0
		if c.character.Tournament == 0 {
			next = 0xFF
		}
		value, err := s.Store.UpdateTournament(c.user.ID, next)
		if err != nil {
			return true, err
		}
		c.character.Tournament = value
		w := &protocol.Writer{}
		userInfo(w, c.character)
		c.send(157, w)
		if value != 0 {
			c.notice("Tournament HUD is ON")
		} else {
			c.notice("Tournament HUD is OFF")
		}
	case "/reqsnapshot":
		c.chatNotice("Not implemented")
	case "/addgroom":
		if err := s.ghostRoom(c); err != nil {
			return true, err
		}
	case "/addwpnreleasechar":
		count, ok := atoiArg(args, 2)
		if len(args) < 3 || !ok || count < 1 || len(args[1]) == 0 {
			c.chatNotice("/addwpnreleasechar usage: /addwpnreleasechar <char> <count>")
			return true, nil
		}
		if err := s.Store.AddWeaponReleaseCharacter(c.user.ID, args[1][0], count); err != nil {
			return true, err
		}
	case "/setitemstatus":
		slot, okSlot := atoiArg(args, 1)
		status, okStatus := atoiArg(args, 2)
		if !okSlot || !okStatus {
			c.chatNotice("/setitemstatus usage: /setitemstatus <slot> <status>")
			return true, nil
		}
		changed, err := s.Store.SetItemStatus(c.user.ID, slot, status)
		if errors.Is(err, store.ErrItemUnavailable) {
			c.chatNotice("Item does not exist")
			return true, nil
		}
		if err != nil {
			return true, err
		}
		if err = s.sendInventoryItems(c, changed); err != nil {
			return true, err
		}
	case "/setiteminuse":
		slot, okSlot := atoiArg(args, 1)
		inUse, okUse := atoiArg(args, 2)
		if !okSlot || !okUse {
			c.chatNotice("/setiteminuse usage: /setiteminuse <slot> <inUse>")
			return true, nil
		}
		changed, err := s.Store.SetItemInUse(c.user.ID, slot, inUse)
		if errors.Is(err, store.ErrItemUnavailable) {
			c.chatNotice("Item does not exist")
			return true, nil
		}
		if err != nil {
			return true, err
		}
		if err = s.sendInventoryItems(c, changed); err != nil {
			return true, err
		}
	case "/addallitems":
		protected := map[int]bool{}
		for _, id := range s.Config.DefaultUser.DefaultItems {
			protected[id] = true
		}
		for _, id := range s.Config.DefaultUser.PseudoDefaultItems {
			protected[id] = true
		}
		ids := make([]int, 0, len(s.items))
		for id := range s.items {
			if !protected[id] {
				ids = append(ids, id)
			}
		}
		sort.Ints(ids)
		rewards := make([]store.Reward, 0, len(ids))
		for _, id := range ids {
			rewards = append(rewards, store.Reward{ID: id, Count: 1})
		}
		items, err := s.prepareGrants(rewards)
		if err != nil {
			return true, err
		}
		changed, err := s.Store.GrantReward(c.user.ID, store.Grant{Items: items}, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
		if err != nil {
			return true, err
		}
		if err = s.sendInventoryItems(c, changed); err != nil {
			return true, err
		}
		c.chatNotice("Done")
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

// gmGrantItem adds one item stack to an account, online or offline.
func (s *Server) gmGrantItem(id, itemID, count, duration int) ([]store.Item, error) {
	if count < 1 {
		count = 1
	}
	items, err := s.prepareGrants([]store.Reward{{ID: itemID, Count: count, Duration: duration}})
	if err != nil {
		return nil, err
	}
	return s.Store.GrantReward(id, store.Grant{Items: items}, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
}

// gmGrantReward applies a catalog reward and notifies the target when online.
func (s *Server) gmGrantReward(id int, p pendingReward) error {
	changed, err := s.Store.GrantReward(id, p.Grant, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
	if err != nil {
		return err
	}
	target := s.users[id]
	if target == nil {
		return nil
	}
	if err = s.sendInventoryItems(target, changed); err != nil {
		return err
	}
	if w := rewardNotice(p.Label, p.NoticeItems, p.Grant); len(w.Data) <= protocol.MaxPayload {
		target.send(67, w)
	}
	return nil
}

// gmCountDuration reads optional <count> <duration> arguments.
func gmCountDuration(args []string, countIndex, durationIndex int) (int, int) {
	count, duration := 1, 0
	if value, ok := atoiArg(args, countIndex); ok && value > 0 {
		count = value
	}
	if value, ok := atoiArg(args, durationIndex); ok {
		duration = value
	}
	return count, duration
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
