package server

import (
	"slices"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func (s *Server) clanIDFor(c *client) (int, error) {
	clan, ok, err := s.Store.ClanForUser(c.user.ID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, nil
	}
	c.character.ClanID = clan.ID
	c.character.ClanName = clan.Name
	c.character.ClanMarkID = clan.MarkID
	return clan.ID, nil
}

func (s *Server) familyBattleInit(r *room, c *client) bool {
	clanID, err := s.clanIDFor(c)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("family battle clan read failed", "user", c.user.ID, "error", err)
		}
		c.notice("DB_QUERY_FAILED")
		return false
	}
	if clanID == 0 {
		c.notice("ROOM_OPEN_FAILED_CLAN_NOT_MEMBER")
		return false
	}
	r.clan1 = clanID
	r.clan2 = 0
	return true
}

func (s *Server) familyBattleJoin(r *room, c *client) bool {
	clanID, err := s.clanIDFor(c)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("family battle clan read failed", "user", c.user.ID, "error", err)
		}
		c.notice("DB_QUERY_FAILED")
		return false
	}
	if clanID == 0 {
		c.notice("ROOM_JOIN_FAILED_CLAN_NOT_MEMBER")
		return false
	}
	if clanID == r.clan1 {
		return true
	}
	if r.clan2 == 0 {
		r.clan2 = clanID
		return true
	}
	if clanID != r.clan2 {
		c.notice("ROOM_JOIN_FAILED_CLAN_OTHER")
		return false
	}
	return true
}

// familyBattleSettings validates a Family Battle settings update against the
// room's members, mirroring the checks in OnRoomUpdateSettings. On success it
// records the two clan slots; on failure it returns the notice text the
// reference sends.
func (s *Server) familyBattleSettings(r *room, st *settings) (string, bool) {
	hostClan, err := s.clanIDFor(r.host)
	if err != nil || hostClan == 0 {
		return "ROOM_SETTING_FAILED_CLAN_NOT_MEMBER", false
	}
	clans := []int{hostClan}
	for _, member := range r.members {
		if member == r.host {
			continue
		}
		id, err := s.clanIDFor(member)
		if err != nil || id == 0 {
			return "ROOM_SETTING_FAILED_NO_CLAN_USER", false
		}
		if slices.Contains(clans, id) {
			continue
		}
		clans = append(clans, id)
		if len(clans) > 2 {
			return "ROOM_SETTING_FAILED_TOO_MANY_CLANS", false
		}
	}
	if st.number(8) != 0 && len(r.members) > st.number(8) {
		return "ROOM_SETTING_FAILED_TOO_MANY_CLAN_MEMBERS", false
	}
	r.clan1 = clans[0]
	r.clan2 = 0
	if len(clans) == 2 {
		r.clan2 = clans[1]
	}
	return "", true
}

// revertRoomSettings replays the current settings with an empty change mask, the
// shape the reference sends when it refuses an update.
func (s *Server) revertRoomSettings(r *room, c *client) {
	w := &protocol.Writer{}
	w.U8(4)
	for range 4 {
		w.U32(0)
	}
	c.send(65, w)
}

// familyBattleParticipant reports whether the user is on the participant list of
// the running match. The reference keeps m_FamilyBattleUsers; the match statistics
// are that list here, because every participant gets an entry when the game starts
// and keeps it through a reconnect.
func familyBattleParticipant(r *room, userID int) bool {
	if r.match == nil {
		return true
	}
	_, ok := r.match.stats[userID]
	return ok
}

// familyBattleTeam picks the side a joining clan belongs to, mirroring the team
// selection in CChannelManager::OnRoomJoinRequest: find a member who has a clan,
// keep that member's team when the clans match, take the opposite team otherwise.
func (s *Server) familyBattleTeam(r *room, clanID int) int {
	for _, member := range r.members {
		if member.character.ClanID == 0 {
			continue
		}
		if member.character.ClanID == clanID {
			return member.team
		}
		if member.team == 1 {
			return 2
		}
		return 1
	}
	return 2
}

// regroupFamilyBattle mirrors the team reassignment OnRoomUpdateSettings performs
// when a room turns Family Battle on: the host's clan keeps the host's team and
// the opposing clan takes the other side.
func (s *Server) regroupFamilyBattle(r *room) {
	if r.settings.number(70) == 0 || r.host == nil {
		return
	}
	hostTeam := r.host.team
	if hostTeam != 1 && hostTeam != 2 {
		hostTeam = 2
	}
	for _, member := range r.members {
		if member == r.host || member.character.ClanID == 0 {
			continue
		}
		team := hostTeam
		if member.character.ClanID != r.clan1 {
			if hostTeam == 1 {
				team = 2
			} else {
				team = 1
			}
		}
		if member.team == team {
			continue
		}
		member.team = team
		w := &protocol.Writer{}
		w.U8(13)
		w.U32(member.user.ID)
		w.U8(team)
		r.broadcast(65, w)
	}
}

// familyBattleRecompute rebuilds the two clan slots after a member leaves or
// the host migrates. The host's clan always occupies slot one.
func (s *Server) familyBattleRecompute(r *room) {
	r.clan1 = 0
	r.clan2 = 0
	if r.settings.number(70) == 0 {
		return
	}
	if r.host != nil {
		if id, err := s.clanIDFor(r.host); err == nil && id != 0 {
			r.clan1 = id
		}
	}
	for _, member := range r.members {
		id, err := s.clanIDFor(member)
		if err != nil || id == 0 || id == r.clan1 {
			continue
		}
		if r.clan2 == 0 {
			r.clan2 = id
		}
	}
}

func (s *Server) familyBattleCanStart(r *room) bool {
	if r.clan1 == 0 || r.clan2 == 0 {
		return false
	}
	first, second := false, false
	for _, member := range r.members {
		id, err := s.clanIDFor(member)
		if err != nil {
			continue
		}
		if id == r.clan1 {
			first = true
		}
		if id == r.clan2 {
			second = true
		}
	}
	return first && second
}

func (s *Server) sendClanBattleNotice(userID, kind int, gameName string, mode, roomID int) {
	members, err := s.Store.ClanMembers(userID, false)
	if err != nil {
		if s.Log != nil {
			s.Log.Warn("clan battle notice members failed", "user", userID, "error", err)
		}
		return
	}
	w := &protocol.Writer{}
	w.U8(116)
	w.U8(kind)
	w.String(gameName)
	w.U8(mode)
	w.U16(roomID)
	for _, member := range members {
		if target := s.users[member.UserID]; target != nil {
			target.send(71, w)
		}
	}
}

// kickClanMembers handles room sub-request 65/37: the family battle host
// removes every member of the opposing clan, then tells each kicked client
// which users were removed.
func (s *Server) kickClanMembers(c *client) error {
	r := s.rooms[c.room]
	if r == nil || r.host != c || c.playing {
		return nil
	}
	if r.settings.number(70) == 0 || r.clan2 == 0 {
		return nil
	}
	hostClan, err := s.clanIDFor(c)
	if err != nil {
		return err
	}
	if hostClan == 0 {
		return nil
	}
	members := append([]*client{}, r.members...)
	var kicked []*client
	for _, member := range members {
		if member == c {
			continue
		}
		clanID, err := s.clanIDFor(member)
		if err != nil {
			continue
		}
		if clanID == 0 || clanID == hostClan {
			continue
		}
		member.send(65, &protocol.Writer{Data: []byte{10}})
		if r.kicked == nil {
			r.kicked = map[int]bool{}
		}
		r.kicked[member.user.ID] = true
		kicked = append(kicked, member)
	}
	if len(kicked) == 0 {
		return nil
	}
	for _, member := range kicked {
		s.leaveRoom(member)
		s.joinLobby(member)
	}
	w := &protocol.Writer{}
	w.U8(37)
	w.U8(len(kicked))
	for _, member := range kicked {
		w.U32(member.user.ID)
	}
	for _, member := range kicked {
		member.send(65, w)
	}
	return nil
}

func (s *Server) roomNoticeClan(c *client) error {
	r := s.rooms[c.room]
	if r == nil || r.settings.number(70) == 0 || c.playing {
		return nil
	}
	clanID, err := s.clanIDFor(c)
	if err != nil {
		return err
	}
	if clanID == 0 {
		return nil
	}
	s.sendClanBattleNotice(c.user.ID, 1, c.character.Name, r.settings.number(6), r.id)
	return nil
}
