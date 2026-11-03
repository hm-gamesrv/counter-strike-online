package server

import (
	"database/sql"
	"errors"
	"time"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

const (
	clanMaxMembers = 450
)

func (s *Server) clanRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	switch kind {
	case 0:
		return s.clanListRequest(c, r)
	case 1:
		return s.clanInfoRequest(c, r)
	case 2:
		return s.clanCreateRequest(c, r)
	case 3:
		return s.clanJoinRequest(c, r)
	case 4:
		return s.clanCancelJoinRequest(c, r)
	case 5:
		return s.clanJoinApproveRequest(c, r)
	case 6:
		return s.clanJoinResultRequest(c, r)
	case 7:
		return s.clanLeaveRequest(c, r)
	case 8:
		return s.clanInviteRequest(c, r)
	case 11:
		return s.clanChangeGradeRequest(c, r)
	case 12:
		return s.clanKickRequest(c, r)
	case 14:
		return s.clanUpdateConfigRequest(c, r)
	case 15:
		return s.clanUpdateMarkRequest(c, r)
	case 16:
		return s.clanSetNoticeRequest(c, r)
	case 24:
		return s.clanStorageGive(c, r)
	case 25:
		return s.clanStorageGet(c, r)
	case 26:
		return s.clanStorageRequest(c, r)
	case 31:
		return s.clanStorageDelete(c, r)
	case 27:
		return s.clanDissolveRequest(c, r)
	case 28:
		return s.clanDelegateMasterRequest(c, r)
	case 29:
		return s.clanMemberListRequest(c, r)
	case 30:
		return s.clanJoinListRequest(c, r)
	case 113:
		return s.clanChatRequest(c, r)
	case 101, 106, 107, 109, 112, 116, 161, 169:
		if s.Log != nil {
			s.Log.Debug("clan server-only packet ignored", "type", kind, "user", c.user.ID)
		}
		return r.Err
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported clan request", "type", kind, "user", c.user.ID)
		}
		return r.Err
	}
}

func clanNameProblem(name string) (int, string) {
	if len(name) < 3 {
		return 0, "CSO_CLAN_CREATE_NAME_TOO_SHORT"
	}
	if len(name) > 20 {
		return 0, "CSO_CLAN_CREATE_NAME_TOO_LONG"
	}
	for _, ch := range name {
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
			continue
		}
		return 0, "CSO_CLAN_CREATE_NAME_BAD_RESERVE"
	}
	return 1, ""
}

func (s *Server) clanListRequest(c *client, r *protocol.Reader) error {
	page := r.U16()
	flag := r.U8()
	gameMode := r.U8()
	unknown := r.U8()
	playTime := r.U8()
	name := r.CString()
	if r.Err != nil {
		return r.Err
	}
	clans, pageMax, err := s.Store.ClanList(name, flag, gameMode, playTime, page)
	if err != nil {
		return err
	}
	w := &protocol.Writer{}
	w.U8(0)
	w.U8(len(clans))
	w.U16(page)
	w.U16(pageMax)
	for _, clan := range clans {
		w.U32(clan.ID)
		w.String(clan.Name)
		w.String(clan.MasterName)
		w.U32(clan.Score)
		w.U32(clan.MarkID)
		w.U16(clan.GameModeID)
		w.U8(clan.Time)
		w.U16(2228)
		w.U32(clan.Region)
		w.U16(clan.MemberCount)
		w.U8(clan.JoinMethod)
		w.String(clan.Notice)
		w.U8(0)
		w.U32(0)
		w.U32(0)
		w.U8(0)
	}
	c.send(71, w)
	if unknown != 0 && s.Log != nil {
		s.Log.Debug("clan list unknown field", "value", unknown, "user", c.user.ID)
	}
	return nil
}

func (s *Server) clanInfoRequest(c *client, r *protocol.Reader) error {
	clanID := r.U32()
	if r.Err != nil {
		return r.Err
	}
	clan, err := s.Store.ClanInfo(clanID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	w := &protocol.Writer{}
	w.U8(1)
	buildClanInfo(w, clan)
	c.send(71, w)
	return nil
}

func (s *Server) clanCreateRequest(c *client, r *protocol.Reader) error {
	name := r.CString()
	if r.Err != nil {
		return r.Err
	}
	if r.Pos != len(r.Data) {
		return errors.New("unexpected clan create tail")
	}
	if ok, text := clanNameProblem(name); ok == 0 {
		s.clanReply(c, 2, 0, text)
		return nil
	}
	clanID, err := s.Store.CreateClan(store.ClanCreate{
		MasterID:        c.user.ID,
		Name:            name,
		Description:     "",
		Notice:          "",
		GameModeID:      0,
		MapID:           0,
		Time:            0,
		Region:          c.character.Nation,
		JoinMethod:      3,
		ExpBonus:        5,
		PointBonus:      5,
		MarkChangeCount: 1,
		MaxMemberCount:  clanMaxMembers,
	}, clanNowMinutes())
	if err != nil {
		return err
	}
	switch clanID {
	case -1:
		s.clanReply(c, 2, 0, "CSO_CLAN_CREATE_DUP_NAME")
		return nil
	case -2:
		s.clanReply(c, 2, 0, "CSO_CLAN_CREATE_ALREADY_IN_CLAN")
		return nil
	case -3:
		s.clanReply(c, 2, 0, "CSO_CLAN_CREATE_MORE_POINT")
		return nil
	case 0:
		s.clanReply(c, 2, 0, "CSO_CLAN_DB_SYSTEM_ERROR")
		return nil
	}
	s.clanReply(c, 2, 1, "")
	if err = s.refreshClanCharacter(c); err != nil {
		return err
	}
	return s.sendClanLogin(c)
}

func (s *Server) clanJoinRequest(c *client, r *protocol.Reader) error {
	clanID := r.U32()
	if r.Err != nil {
		return r.Err
	}
	code, other, err := s.Store.JoinClan(c.user.ID, clanID, clanNowMinutes())
	if err != nil {
		return err
	}
	switch code {
	case 0:
		s.clanJoinReply(c, 4, "CSO_CLAN_DB_SYSTEM_ERROR")
		return nil
	case -1:
		s.clanJoinReply(c, 4, "CSO_CLAN_JOIN_EXIST_MEMBER")
		return nil
	case -2:
		s.clanJoinReply(c, 4, "CSO_CLAN_JOIN_SAME_CLAN")
		return nil
	case -3:
		s.clanJoinReply(c, 4, "CSO_CLAN_JOIN_NOT_ALLOWED")
		return nil
	case -4:
		s.clanJoinReply(c, 4, "CSO_CLAN_JOIN_NEED_INVITE")
		return nil
	case -5:
		s.clanJoinReply(c, 4, "CSO_CLAN_JOIN_NO_AUTH")
		return nil
	case -6:
		s.clanJoinReply(c, 4, "CSO_CLAN_JOIN_MAX_MEMBER")
		return nil
	case -7:
		s.clanJoinReply(c, 2, other)
		return nil
	}
	s.clanJoinReply(c, 0, "")
	if err = s.refreshClanCharacter(c); err != nil {
		return err
	}
	return s.sendClanLogin(c)
}

func (s *Server) clanCancelJoinRequest(c *client, r *protocol.Reader) error {
	clanID := r.U32()
	if r.Err != nil {
		return r.Err
	}
	code, err := s.Store.CancelJoinClan(c.user.ID, clanID)
	if err != nil {
		return err
	}
	if code != 1 {
		s.clanReply(c, 4, 0, "CSO_CLAN_JOIN_CANCEL_NOT_APPLIED")
		return nil
	}
	s.clanReply(c, 4, 1, "")
	return nil
}

func (s *Server) clanJoinApproveRequest(c *client, r *protocol.Reader) error {
	name := r.CString()
	if r.Err != nil {
		return r.Err
	}
	code, targetID, err := s.Store.ApproveClanJoin(c.user.ID, name, clanNowMinutes())
	if err != nil {
		return err
	}
	if code == -6 {
		s.clanReply(c, 5, 0, "CSO_CLAN_JOIN_MAX_MEMBER")
		return nil
	}
	if code != 1 {
		s.clanReply(c, 5, 0, "CSO_CLAN_NOT_IN_APPLICANTS")
		return nil
	}
	s.clanReply(c, 5, 1, "")
	s.sendClanJoinUpdate(c, name, true)
	if target := s.users[targetID]; target != nil {
		if err = s.refreshClanCharacter(target); err != nil {
			return err
		}
		if err = s.sendClanLogin(target); err != nil {
			return err
		}
	}
	if err = s.sendClanMemberList(c); err != nil {
		return err
	}
	return nil
}

func (s *Server) clanJoinResultRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	switch kind {
	case 0:
		name := r.CString()
		if r.Err != nil {
			return r.Err
		}
		code, err := s.Store.RejectClanJoin(c.user.ID, name)
		if err != nil {
			return err
		}
		if code != 1 {
			s.clanReply(c, 6, 0, "CSO_CLAN_NOT_IN_APPLICANTS")
			return nil
		}
		s.clanReply(c, 6, 1, "")
		s.sendClanJoinUpdate(c, name, true)
	case 1:
		code, err := s.Store.RejectAllClanJoins(c.user.ID)
		if err != nil {
			return err
		}
		if code != 1 {
			s.clanReply(c, 6, 0, "CSO_CLAN_NOT_IN_APPLICANTS")
			return nil
		}
		s.clanReply(c, 6, 1, "")
		s.sendClanJoinDelete(c)
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported clan join result", "kind", kind, "user", c.user.ID)
		}
	}
	return r.Err
}

func (s *Server) clanLeaveRequest(c *client, r *protocol.Reader) error {
	if r.Err != nil {
		return r.Err
	}
	members, err := s.Store.ClanMembers(c.user.ID, false)
	if err != nil {
		return err
	}
	code, err := s.Store.LeaveClan(c.user.ID)
	if err != nil {
		return err
	}
	if code != 1 {
		s.clanReply(c, 7, 0, "CSO_CLAN_NO_AUTH")
		return nil
	}
	s.clanReply(c, 7, 1, "")
	left := store.ClanMember{
		UserID:   c.user.ID,
		UserName: c.user.Name,
		GameName: c.character.Name,
	}
	for _, member := range members {
		if member.UserID == c.user.ID {
			continue
		}
		if target := s.users[member.UserID]; target != nil {
			s.sendClanUserUpdate(target, left, true)
			s.sendClanMemberUpdate(target, left, true)
		}
	}
	return s.refreshClanCharacter(c)
}

func (s *Server) clanInviteRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	if kind == 1 {
		return r.Err
	}
	name := r.CString()
	if r.Err != nil {
		return r.Err
	}
	code, targetID, inviter, err := s.Store.InviteToClan(c.user.ID, name)
	if err != nil {
		return err
	}
	switch code {
	case 0:
		s.clanReply(c, 8, 0, "CSO_CLAN_DB_SYSTEM_ERROR")
		return nil
	case -1:
		s.clanReply(c, 8, 0, "CSO_CLAN_INVITE_NO_AUTH")
		return nil
	case -3:
		s.clanReply(c, 8, 0, "CSO_CLAN_INVITE_FAIL_NOT_EXIST")
		return nil
	case -4:
		s.clanReply(c, 8, 0, "CSO_CLAN_CANT_INVITE_MASTER")
		return nil
	case -5:
		s.clanReply(c, 8, 0, "CSO_CLAN_CANT_INVITE_SAME_CLAN")
		return nil
	}
	target := s.users[targetID]
	if target == nil {
		s.clanReply(c, 8, 0, "CSO_CLAN_INVITE_FAILED")
		return nil
	}
	clan, ok, err := s.Store.ClanForUser(c.user.ID)
	if err != nil {
		return err
	}
	if !ok {
		s.clanReply(c, 8, 0, "CSO_CLAN_INVITE_NO_AUTH")
		return nil
	}
	w := &protocol.Writer{}
	w.U8(112)
	w.String(inviter)
	w.U32(clan.ID)
	target.send(71, w)
	s.clanReply(c, 8, 1, "")
	return nil
}

func (s *Server) clanChangeGradeRequest(c *client, r *protocol.Reader) error {
	name := r.CString()
	grade := r.U8()
	if r.Err != nil {
		return r.Err
	}
	if grade > 3 {
		return nil
	}
	code, targetID, err := s.Store.SetClanGrade(c.user.ID, name, grade)
	if err != nil {
		return err
	}
	if code != 1 {
		s.clanReply(c, 11, 0, "CSO_CLAN_GRADE_NO_AUTH")
		return nil
	}
	clan, ok, err := s.Store.ClanForUser(c.user.ID)
	if err != nil {
		return err
	}
	if ok {
		if target := s.users[targetID]; target != nil {
			s.sendClanUpdate(target, 1, grade, clan)
		}
	}
	if err = s.sendClanMemberList(c); err != nil {
		return err
	}
	s.clanReply(c, 11, 1, "")
	return nil
}

func (s *Server) clanKickRequest(c *client, r *protocol.Reader) error {
	name := r.CString()
	if r.Err != nil {
		return r.Err
	}
	members, err := s.Store.ClanMembers(c.user.ID, false)
	if err != nil {
		return err
	}
	code, targetID, err := s.Store.KickClanMember(c.user.ID, name)
	if err != nil {
		return err
	}
	if code != 1 {
		s.clanReply(c, 12, 0, "CSO_CLAN_NO_AUTH")
		return nil
	}
	s.clanReply(c, 12, 1, "")
	removed := store.ClanMember{
		UserID: targetID,
	}
	for _, member := range members {
		if member.UserID == targetID {
			removed = member
			break
		}
	}
	for _, member := range members {
		if member.UserID == targetID || member.UserID == c.user.ID {
			continue
		}
		if target := s.users[member.UserID]; target != nil {
			s.sendClanUserUpdate(target, removed, true)
			s.sendClanMemberUpdate(target, removed, true)
		}
	}
	if target := s.users[targetID]; target != nil {
		w := &protocol.Writer{}
		w.U8(106)
		w.U8(0)
		target.send(71, w)
		target.character.ClanID = 0
		target.character.ClanName = ""
		target.character.ClanMarkID = 0
		s.broadcastUserInfo(target)
	}
	if err = s.sendClanMemberList(c); err != nil {
		return err
	}
	return nil
}

func (s *Server) clanUpdateConfigRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	switch kind {
	case 0:
		method := r.U8()
		if r.Err != nil {
			return r.Err
		}
		if method > 3 {
			return nil
		}
		code, err := s.Store.SetClanJoinMethod(c.user.ID, method)
		if err != nil {
			return err
		}
		if code != 1 {
			s.clanReply(c, 14, 0, "CSO_CLAN_SETTING_NO_AUTH")
			return nil
		}
		s.clanReply(c, 14, 1, "")
		if clan, ok, err := s.Store.ClanForUser(c.user.ID); err != nil {
			return err
		} else if ok {
			_, grade, err := s.Store.ClanGrade(c.user.ID)
			if err != nil {
				return err
			}
			s.sendClanUpdate(c, 1, grade, clan)
		}
	case 3:
		region := r.U16()
		if r.Err != nil {
			return r.Err
		}
		code, err := s.Store.SetClanRegion(c.user.ID, region)
		if err != nil {
			return err
		}
		if code != 1 {
			s.clanReply(c, 14, 0, "CSO_CLAN_SETTING_NO_AUTH")
			return nil
		}
		s.clanReply(c, 14, 1, "")
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported clan config", "kind", kind, "user", c.user.ID)
		}
	}
	return r.Err
}

func (s *Server) clanSetNoticeRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	if kind != 1 {
		return r.Err
	}
	notice := r.CString()
	if r.Err != nil {
		return r.Err
	}
	if len(notice) > 255 {
		return errors.New("clan notice too long")
	}
	code, err := s.Store.SetClanNotice(c.user.ID, notice)
	if err != nil {
		return err
	}
	if code != 1 {
		s.clanReply(c, 16, 0, "CSO_CLAN_NOTICE_NO_AUTH")
		return nil
	}
	s.clanReply(c, 16, 1, "")
	clan, ok, err := s.Store.ClanForUser(c.user.ID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	members, err := s.Store.ClanMembers(c.user.ID, false)
	if err != nil {
		return err
	}
	for _, member := range members {
		if target := s.users[member.UserID]; target != nil {
			s.sendClanNotice(target, clan)
		}
	}
	return nil
}

func (s *Server) clanUpdateMarkRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	if kind != 0 {
		return r.Err
	}
	sub := r.U8()
	switch sub {
	case 0:
		r.U16()
		r.U8()
		if r.Err != nil {
			return r.Err
		}
		s.sendClanMarkColors(c)
	case 1:
		markID := r.U32()
		if r.Err != nil {
			return r.Err
		}
		code, err := s.Store.SetClanMark(c.user.ID, markID)
		if err != nil {
			return err
		}
		switch code {
		case -2:
			s.clanMarkReply(c, 0, "CSO_CLAN_NO_MARK_ITEM")
		case -1:
			s.clanMarkReply(c, 0, "CSO_CLAN_GRADE_NO_AUTH")
		case 2:
			s.clanMarkReply(c, 0, "CSO_CLAN_MARK_DUP")
		case 0:
			s.clanMarkReply(c, 0, "CSO_CLAN_DB_SYSTEM_ERROR")
		case 1:
			s.clanMarkReply(c, 1, "")
			if clan, ok, err := s.Store.ClanForUser(c.user.ID); err != nil {
				return err
			} else if ok {
				s.sendClanUpdate(c, 7, 0, clan)
			}
			if err = s.refreshClanCharacter(c); err != nil {
				return err
			}
		}
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported clan mark", "sub", sub, "user", c.user.ID)
		}
	}
	return r.Err
}

func (s *Server) clanDissolveRequest(c *client, r *protocol.Reader) error {
	code, members, err := s.Store.DissolveClan(c.user.ID)
	if err != nil {
		return err
	}
	if code != 1 {
		s.clanReply(c, 27, 0, "CSO_CLAN_NO_AUTH")
		return nil
	}
	s.clanReply(c, 27, 1, "")
	for _, id := range members {
		target := s.users[id]
		if target == nil {
			continue
		}
		target.character.ClanID = 0
		target.character.ClanName = ""
		target.character.ClanMarkID = 0
		target.notice("Clan disbanded.")
		s.broadcastUserInfo(target)
	}
	return nil
}

func (s *Server) clanDelegateMasterRequest(c *client, r *protocol.Reader) error {
	name := r.CString()
	if r.Err != nil {
		return r.Err
	}
	code, targetID, err := s.Store.DelegateClanMaster(c.user.ID, name, clanNowMinutes())
	if err != nil {
		return err
	}
	w := &protocol.Writer{}
	w.U8(28)
	w.U8(0)
	w.String("KAVO SHO ALO")
	c.send(71, w)
	if code == 1 {
		if target := s.users[targetID]; target != nil {
			if err = s.refreshClanCharacter(target); err != nil {
				return err
			}
			if err = s.sendClanLogin(target); err != nil {
				return err
			}
		}
		if err = s.refreshClanCharacter(c); err != nil {
			return err
		}
		if err = s.sendClanLogin(c); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) clanMemberListRequest(c *client, r *protocol.Reader) error {
	members, err := s.Store.ClanMembers(c.user.ID, true)
	if err != nil {
		return err
	}
	if len(members) != 0 {
		s.sendClanMemberListData(c, members)
	}
	return r.Err
}

func (s *Server) clanJoinListRequest(c *client, r *protocol.Reader) error {
	requests, err := s.Store.ClanJoinRequests(c.user.ID)
	if err != nil {
		return err
	}
	if len(requests) != 0 {
		s.sendClanJoinListData(c, requests)
	}
	return r.Err
}

func (s *Server) clanChatRequest(c *client, r *protocol.Reader) error {
	text := r.CString()
	if r.Err != nil {
		return r.Err
	}
	if len(text) > 1024 {
		return errors.New("clan chat too long")
	}
	members, err := s.Store.ClanMembers(c.user.ID, false)
	if err != nil {
		return err
	}
	if len(members) == 0 {
		return nil
	}
	w := &protocol.Writer{}
	w.U8(113)
	w.String(c.character.Name)
	w.String(text)
	for _, member := range members {
		target := s.users[member.UserID]
		if target == nil {
			continue
		}
		if s.chatAllowed(c, target) {
			target.send(71, w)
		}
	}
	return nil
}

func (s *Server) clanStorageGive(c *client, r *protocol.Reader) error {
	page := r.U8()
	r.U8()
	slot := r.U16()
	if r.Err != nil {
		return r.Err
	}
	items, code, err := s.Store.GiveClanItem(c.user.ID, page, slot)
	if err != nil {
		return err
	}
	switch code {
	case -3:
		s.clanReply(c, 24, 0, "CSO_CLAN_GIVE_ITEM_EXCEED_LIMIT")
		return nil
	case -1, -2, 0:
		s.clanReply(c, 24, 0, "CSO_CLAN_DB_SYSTEM_ERROR")
		return nil
	}
	if err = s.sendInventoryItems(c, items); err != nil {
		return err
	}
	// A deposited copy can be the equipped nameplate or effect; the store cleared
	// the selection, so the online cache and the client's user info must follow.
	s.refreshCharacterCache(c)
	s.clanReply(c, 24, 1, "")
	return s.sendClanStoragePage(c, page)
}

func (s *Server) clanStorageGet(c *client, r *protocol.Reader) error {
	page := r.U8()
	slot := r.U16()
	if r.Err != nil {
		return r.Err
	}
	items, code, err := s.Store.TakeClanItem(c.user.ID, page, slot, s.Config.InventorySlotMax, len(s.Config.DefaultUser.DefaultItems))
	if err != nil {
		return err
	}
	switch code {
	case -2:
		s.clanReply(c, 25, 0, "CSO_CLAN_INVENTORY_FULL")
		return nil
	case -3:
		s.clanReply(c, 25, 0, "CSO_CLAN_GET_ITEM_HAS_INFINITY_ITEM")
		return nil
	case -4:
		s.clanReply(c, 25, 0, "CSO_CLAN_GET_ITEM_EXCEED_LIMIT")
		return nil
	case -5:
		s.clanReply(c, 25, 0, "CSO_CLAN_GET_ITEM_HAS_CLAN_ITEM")
		return nil
	case -1, 0:
		s.clanReply(c, 25, 0, "CSO_CLAN_SYSTEM_ERROR")
		return nil
	}
	if err = s.sendInventoryItems(c, items); err != nil {
		return err
	}
	s.clanReply(c, 25, 1, "")
	return s.sendClanStoragePage(c, page)
}

func (s *Server) clanStorageDelete(c *client, r *protocol.Reader) error {
	page := r.U8()
	slot := r.U16()
	if r.Err != nil {
		return r.Err
	}
	code, err := s.Store.DeleteClanItem(c.user.ID, page, slot)
	if err != nil {
		return err
	}
	if code != 1 {
		s.clanReply(c, 31, 0, "CSO_CLAN_DB_SYSTEM_ERROR")
		return nil
	}
	s.clanReply(c, 31, 1, "")
	return s.sendClanStoragePage(c, page)
}

func (s *Server) clanStorageRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	switch kind {
	case 0:
		page := r.U8()
		if r.Err != nil {
			return r.Err
		}
		return s.sendClanStoragePage(c, page)
	case 1:
		s.sendClanStorageHistory(c)
	case 2:
		page := r.U8()
		grade := r.U8()
		if r.Err != nil {
			return r.Err
		}
		code, err := s.Store.SetClanStorageAccess(c.user.ID, page, grade)
		if err != nil {
			return err
		}
		if code != 1 {
			s.sendClanStorageReply(c, 0, "CSO_CLAN_DB_SYSTEM_ERROR")
			return nil
		}
		s.sendClanStorageReply(c, 1, "")
		grades, err := s.Store.ClanStorageAccess(c.user.ID)
		if err != nil {
			return err
		}
		members, err := s.Store.ClanMembers(c.user.ID, false)
		if err != nil {
			return err
		}
		for _, member := range members {
			if target := s.users[member.UserID]; target != nil {
				s.sendClanStorageAccessData(target, grades)
			}
		}
	case 3:
		grades, err := s.Store.ClanStorageAccess(c.user.ID)
		if err != nil {
			return err
		}
		s.sendClanStorageAccessData(c, grades)
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported clan storage request", "kind", kind, "user", c.user.ID)
		}
	}
	return r.Err
}

func (s *Server) sendClanStoragePage(c *client, page int) error {
	items, err := s.Store.ClanStoragePage(c.user.ID, page)
	if err != nil {
		return err
	}
	w := &protocol.Writer{}
	w.U8(26)
	w.U8(0)
	w.U8(page)
	buildClanStorage(w, items)
	c.send(71, w)
	return nil
}

func (s *Server) sendClanStorageHistory(c *client) {
	w := &protocol.Writer{}
	w.U8(26)
	w.U8(1)
	w.U16(0)
	c.send(71, w)
}

func (s *Server) sendClanStorageReply(c *client, code int, text string) {
	w := &protocol.Writer{}
	w.U8(26)
	w.U8(2)
	w.U8(code)
	if code == 0 {
		w.String(text)
	}
	c.send(71, w)
}

func (s *Server) sendClanStorageAccessData(c *client, grades []int) {
	w := &protocol.Writer{}
	w.U8(26)
	w.U8(3)
	for _, grade := range grades {
		w.U8(grade)
	}
	c.send(71, w)
}

func (s *Server) sendClanLogin(c *client) error {
	clan, ok, err := s.Store.ClanForUser(c.user.ID)
	if err != nil || !ok {
		return err
	}
	_, grade, err := s.Store.ClanGrade(c.user.ID)
	if err != nil {
		return err
	}
	c.character.ClanID = clan.ID
	c.character.ClanName = clan.Name
	c.character.ClanMarkID = clan.MarkID
	members, err := s.Store.ClanMembers(c.user.ID, false)
	if err != nil {
		return err
	}
	self := store.ClanMember{
		UserID:   c.user.ID,
		UserName: c.user.Name,
		GameName: c.character.Name,
		Grade:    grade,
	}
	for _, member := range members {
		if member.UserID == c.user.ID {
			self = member
			continue
		}
		if target := s.users[member.UserID]; target != nil {
			s.sendClanUserUpdate(target, self, false)
		}
	}
	s.sendClanUserListData(c, members)
	grades, err := s.Store.ClanStorageAccess(c.user.ID)
	if err != nil {
		return err
	}
	if len(grades) != 0 {
		s.sendClanStorageAccessData(c, grades)
	}
	if grade <= 1 {
		if err = s.sendClanMemberList(c); err != nil {
			return err
		}
		if err = s.sendClanJoinList(c); err != nil {
			return err
		}
	}
	s.sendClanUpdate(c, 0, grade, clan)
	return nil
}

func (s *Server) refreshClanCharacter(c *client) error {
	character, err := s.Store.Character(c.user.ID)
	if err != nil {
		return err
	}
	c.character = character
	s.broadcastUserInfo(c)
	return nil
}

func clanNowMinutes() int {
	return int(time.Now().Unix() / 60)
}

func (s *Server) clanReply(c *client, kind, code int, text string) {
	w := &protocol.Writer{}
	w.U8(kind)
	w.U8(code)
	if code == 0 {
		w.String(text)
	}
	c.send(71, w)
}

func (s *Server) clanJoinReply(c *client, code int, text string) {
	w := &protocol.Writer{}
	w.U8(3)
	w.U8(code)
	if code == 2 || code == 4 {
		w.String(text)
	} else {
		w.U8(0)
	}
	c.send(71, w)
}

func (s *Server) sendClanUpdate(c *client, kind, grade int, clan store.Clan) {
	w := &protocol.Writer{}
	w.U8(107)
	w.U8(kind)
	switch kind {
	case 0:
		w.U8(grade)
		w.U8(clan.JoinMethod)
		w.U8(0)
		buildClanInfo(w, clan)
	case 1:
		w.U8(grade)
		w.U8(clan.JoinMethod)
		w.U8(0)
		w.U32(clan.ID)
		w.String(clan.Name)
		w.String(clan.MasterName)
	case 2:
		w.U16(clan.GameModeID)
		w.U16(clan.MapID)
		w.U8(clan.Time)
	case 3:
		w.U32(clan.MemberCount)
		w.U32(clan.MaxMemberCount)
	case 4:
		for i := 0; i < 2; i++ {
			w.U32(0)
			w.U32(0)
		}
	case 5:
		w.U32(0)
		w.U32(0)
	case 6:
		buildClanChronicle(w, clan.Chronicle)
	case 7:
		w.U32(clan.MarkID)
		w.U8(clan.MarkChangeCount)
	case 8:
		w.U16(clan.ExpBonus)
		w.U16(clan.PointBonus)
		w.U16(22)
		w.U32(33)
		w.U16(44)
	case 9:
		w.String(clan.Notice)
	case 10:
		w.U8(0)
	case 11:
		buildClanStorage(w, clan.LastItems)
	case 12:
		w.U8(0)
		w.U8(0)
	case 13:
		w.U32(0)
		w.U8(0)
		w.U8(0)
	}
	c.send(71, w)
}

func buildClanChronicle(w *protocol.Writer, entries []store.ClanChronicle) {
	w.U32(len(entries))
	for _, entry := range entries {
		w.U32(entry.Date)
		w.U8(entry.Type)
		w.U8(1)
		w.U8(1)
		w.String(entry.Text)
	}
}

func buildClanStorage(w *protocol.Writer, items []store.ClanStorageItem) {
	w.U16(len(items))
	for _, item := range items {
		w.U16(0)
		w.U8(1)
		w.U16(item.ItemID)
		w.U16(item.Count)
		w.U32(item.Expiry)
		w.U16(0)
		w.U32(0)
		w.U16(0)
		w.U8(0)
	}
}

func buildClanInfo(w *protocol.Writer, clan store.Clan) {
	w.U32(clan.ID)
	w.String(clan.Name)
	w.String(clan.MasterName)
	w.U64(int64(clan.GameModeID))
	w.U64(int64(clan.MapID))
	w.U8(0)
	w.U8(0)
	w.U8(0)
	w.U32(clan.MemberCount)
	w.U32(clan.MaxMemberCount)
	w.U32(0)
	w.U32(0)
	for i := 0; i < 2; i++ {
		w.U32(0)
		w.U32(0)
	}
	w.U32(0)
	w.U32(0)
	buildClanChronicle(w, clan.Chronicle)
	w.U32(clan.MarkID)
	w.U8(clan.MarkChangeCount)
	w.U16(clan.ExpBonus)
	w.U16(clan.PointBonus)
	w.String(clan.Notice)
	w.U8(0)
	buildClanStorage(w, clan.LastItems)
	w.U8(0)
	w.U8(0)
	w.U32(0)
	w.U8(0)
	w.U8(0)
	w.U16(0)
	w.U32(0)
	w.U16(0)
}

func (s *Server) sendClanUserListData(c *client, members []store.ClanMember) {
	w := &protocol.Writer{}
	w.U8(109)
	w.U8(0)
	w.U16(len(members))
	for _, member := range members {
		w.String(member.UserName)
		w.String(member.GameName)
		w.U32(member.UserID)
		w.U8(boolInt(s.users[member.UserID] != nil))
		w.U32(0)
		w.U8(0)
	}
	c.send(71, w)
}

func (s *Server) sendClanUserUpdate(c *client, member store.ClanMember, remove bool) {
	w := &protocol.Writer{}
	w.U8(109)
	if remove {
		w.U8(2)
		w.String(member.UserName)
	} else {
		w.U8(1)
		w.String(member.UserName)
		w.String(member.GameName)
		w.U32(member.UserID)
		w.U8(boolInt(s.users[member.UserID] != nil))
		w.U32(0)
		w.U8(0)
	}
	c.send(71, w)
}

func (s *Server) sendClanMemberList(c *client) error {
	members, err := s.Store.ClanMembers(c.user.ID, true)
	if err != nil {
		return err
	}
	s.sendClanMemberListData(c, members)
	return nil
}

func (s *Server) sendClanMemberListData(c *client, members []store.ClanMember) {
	w := &protocol.Writer{}
	w.U8(29)
	w.U8(0)
	w.U16(len(members))
	for _, member := range members {
		w.U32(member.Level)
		w.String(member.UserName)
		w.String(member.GameName)
		w.U8(member.Grade)
		w.U32(member.Kills)
		w.U32(member.Deaths)
		w.U32(0)
		w.U32(0)
		w.U32(0)
		w.U32(0)
	}
	c.send(71, w)
}

func (s *Server) sendClanMemberUpdate(c *client, member store.ClanMember, remove bool) {
	w := &protocol.Writer{}
	w.U8(29)
	if remove {
		w.U8(2)
		w.String(member.UserName)
	} else {
		w.U8(1)
		w.U32(member.Level)
		w.String(member.UserName)
		w.String(member.GameName)
		w.U8(member.Grade)
		w.U32(member.Kills)
		w.U32(member.Deaths)
		w.U32(0)
		w.U32(0)
		w.U32(0)
		w.U32(0)
	}
	c.send(71, w)
}

func (s *Server) sendClanJoinList(c *client) error {
	requests, err := s.Store.ClanJoinRequests(c.user.ID)
	if err != nil {
		return err
	}
	s.sendClanJoinListData(c, requests)
	return nil
}

func (s *Server) sendClanJoinListData(c *client, requests []store.ClanJoinRequest) {
	w := &protocol.Writer{}
	w.U8(30)
	w.U8(0)
	w.U8(len(requests))
	for _, request := range requests {
		w.U32(request.Level)
		w.String(request.UserName)
		w.String(request.GameName)
		w.String(request.InviterName)
		w.U32(228)
		w.U32(request.Kills)
		w.U32(request.Deaths)
		w.U32(0)
		w.U32(0)
		w.U32(0)
		w.U32(0)
		w.U32(request.Date)
	}
	c.send(71, w)
}

func (s *Server) sendClanJoinUpdate(c *client, name string, remove bool) {
	w := &protocol.Writer{}
	w.U8(30)
	w.U8(2)
	w.String(name)
	c.send(71, w)
}

func (s *Server) sendClanJoinDelete(c *client) {
	w := &protocol.Writer{}
	w.U8(30)
	w.U8(3)
	c.send(71, w)
}

func (s *Server) sendClanNotice(c *client, clan store.Clan) {
	w := &protocol.Writer{}
	w.U8(101)
	w.String("UNK2")
	w.String(clan.Notice)
	w.U32(0)
	c.send(71, w)
}

func (s *Server) clanMarkReply(c *client, code int, text string) {
	w := &protocol.Writer{}
	w.U8(15)
	w.U8(0)
	w.U8(1)
	w.U8(code)
	if code == 0 {
		w.String(text)
	}
	c.send(71, w)
}

func (s *Server) sendClanMarkColors(c *client) {
	w := &protocol.Writer{}
	w.U8(15)
	w.U8(0)
	w.U8(0)
	w.U16(0x8CA)
	for i := 0; i < 0x8CA; i++ {
		w.U8(0)
	}
	c.send(71, w)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
