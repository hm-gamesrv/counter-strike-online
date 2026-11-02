package store

import (
	"database/sql"
	"errors"
	"time"
)

const (
	ClanCreateCost      = 30000
	ClanDefaultMax      = 450
	ClanStoragePages    = 5
	ClanStoragePageSize = 20
)

var ErrClanPermission = errors.New("clan permission denied")

type ClanSummary struct {
	ID          int
	Name        string
	MasterName  string
	Score       int
	MarkID      int
	GameModeID  int
	Time        int
	Region      int
	MemberCount int
	JoinMethod  int
	Notice      string
}

type ClanChronicle struct {
	Date int
	Type int
	Text string
}

type ClanStorageItem struct {
	PageID  int
	Slot    int
	ItemID  int
	Count   int
	Expiry  int
	Enhance int
}

type Clan struct {
	ID              int
	MasterUserID    int
	Name            string
	Description     string
	Notice          string
	GameModeID      int
	MapID           int
	Time            int
	MemberCount     int
	ExpBonus        int
	PointBonus      int
	Region          int
	JoinMethod      int
	Score           int
	MarkID          int
	MarkColor       int
	MarkChangeCount int
	MaxMemberCount  int
	MasterName      string
	LastItems       []ClanStorageItem
	Chronicle       []ClanChronicle
}

type ClanMember struct {
	UserID   int
	UserName string
	GameName string
	Level    int
	Kills    int
	Deaths   int
	Grade    int
}

type ClanJoinRequest struct {
	UserID      int
	UserName    string
	GameName    string
	Level       int
	Kills       int
	Deaths      int
	InviterName string
	Date        int
}

type ClanCreate struct {
	MasterID        int
	Name            string
	Description     string
	Notice          string
	GameModeID      int
	MapID           int
	Time            int
	Region          int
	JoinMethod      int
	ExpBonus        int
	PointBonus      int
	MarkChangeCount int
	MaxMemberCount  int
}

func clanDate(now int) int {
	stamp := time.Unix(int64(now)*60, 0).Format("20060102")
	value := 0
	for _, digit := range stamp {
		value = value*10 + int(digit-'0')
	}
	return value
}

// ClanList mirrors the four upstream filter modes. The name filter is only
// applied in mode 0, matching the client's search tab.
func (s *Store) ClanList(name string, flag, gameModeID, playTime, page int) ([]ClanSummary, int, error) {
	where := "1=1"
	args := []any{}
	switch flag {
	case 0:
		if name != "" {
			where += " AND c.name LIKE ?"
			args = append(args, "%"+name+"%")
		}
	case 1:
		where += " AND c.gameModeID=?"
		args = append(args, gameModeID)
	case 2:
		where += " AND c.time=?"
		args = append(args, playTime)
	case 3:
		where += " AND c.gameModeID=? AND c.time=?"
		args = append(args, gameModeID, playTime)
	}
	if page < 0 {
		page = 0
	}
	query := "SELECT c.clanID,c.name,coalesce(u.gameName,''),c.score,c.markID,c.gameModeID,c.time,c.region,c.memberCount,c.joinMethod,coalesce(c.notice,'') FROM Clan c LEFT JOIN UserCharacter u ON u.userID=c.masterUserID WHERE " + where + " ORDER BY c.score DESC LIMIT 15 OFFSET ?"
	rows, err := s.DB.Query(query, append(args, page*15)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var clans []ClanSummary
	for rows.Next() {
		var clan ClanSummary
		if err = rows.Scan(&clan.ID, &clan.Name, &clan.MasterName, &clan.Score, &clan.MarkID, &clan.GameModeID, &clan.Time, &clan.Region, &clan.MemberCount, &clan.JoinMethod, &clan.Notice); err != nil {
			return nil, 0, err
		}
		clans = append(clans, clan)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, err
	}
	var count int
	if err = s.DB.QueryRow("SELECT count(*) FROM Clan c WHERE "+where, args...).Scan(&count); err != nil {
		return nil, 0, err
	}
	pageMax := (count + 14) / 15
	if pageMax < 1 {
		pageMax = 1
	}
	return clans, pageMax, nil
}

// ClanInfo reads the clan header, the ten newest storage rows and the full
// chronicle. A missing clan returns sql.ErrNoRows.
func (s *Store) ClanInfo(clanID int) (Clan, error) {
	var clan Clan
	err := s.DB.QueryRow("SELECT c.clanID,c.masterUserID,c.name,coalesce(c.description,''),coalesce(c.notice,''),c.gameModeID,c.mapID,c.time,c.memberCount,c.expBonus,c.pointBonus,c.region,c.joinMethod,c.score,c.markID,c.markColor,c.markChangeCount,c.maxMemberCount,coalesce(u.gameName,'') FROM Clan c LEFT JOIN UserCharacter u ON u.userID=c.masterUserID WHERE c.clanID=?", clanID).Scan(&clan.ID, &clan.MasterUserID, &clan.Name, &clan.Description, &clan.Notice, &clan.GameModeID, &clan.MapID, &clan.Time, &clan.MemberCount, &clan.ExpBonus, &clan.PointBonus, &clan.Region, &clan.JoinMethod, &clan.Score, &clan.MarkID, &clan.MarkColor, &clan.MarkChangeCount, &clan.MaxMemberCount, &clan.MasterName)
	if err != nil {
		return clan, err
	}
	rows, err := s.DB.Query("SELECT pageID,slot,itemID,itemCount,itemDuration,itemEnhValue FROM ClanStorageItem WHERE clanID=? AND itemID<>0 ORDER BY pageID,slot LIMIT 10", clanID)
	if err != nil {
		return clan, err
	}
	for rows.Next() {
		var item ClanStorageItem
		if err = rows.Scan(&item.PageID, &item.Slot, &item.ItemID, &item.Count, &item.Expiry, &item.Enhance); err != nil {
			rows.Close()
			return clan, err
		}
		clan.LastItems = append(clan.LastItems, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return clan, err
	}
	rows, err = s.DB.Query("SELECT date,type,coalesce(string,'') FROM ClanChronicle WHERE clanID=? ORDER BY date DESC LIMIT 32", clanID)
	if err != nil {
		return clan, err
	}
	for rows.Next() {
		var entry ClanChronicle
		if err = rows.Scan(&entry.Date, &entry.Type, &entry.Text); err != nil {
			rows.Close()
			return clan, err
		}
		clan.Chronicle = append(clan.Chronicle, entry)
	}
	err = rows.Err()
	rows.Close()
	return clan, err
}

func (s *Store) ClanForUser(userID int) (Clan, bool, error) {
	var clanID int
	err := s.DB.QueryRow("SELECT clanID FROM ClanMember WHERE userID=?", userID).Scan(&clanID)
	if errors.Is(err, sql.ErrNoRows) {
		return Clan{}, false, nil
	}
	if err != nil {
		return Clan{}, false, err
	}
	clan, err := s.ClanInfo(clanID)
	if errors.Is(err, sql.ErrNoRows) {
		return Clan{}, false, nil
	}
	if err != nil {
		return Clan{}, false, err
	}
	return clan, true, nil
}

// CreateClan deducts the creation cost, allocates a clan ID from UserDist,
// creates the five storage pages and records the first chronicle entry.
// Result codes follow upstream: -1 duplicate name, -2 already in a clan or
// pending request, -3 not enough points, 0 database error, positive clan ID.
func (s *Store) CreateClan(cfg ClanCreate, now int) (int, error) {
	if cfg.MaxMemberCount <= 0 {
		cfg.MaxMemberCount = ClanDefaultMax
	}
	if cfg.JoinMethod < 0 || cfg.JoinMethod > 3 || cfg.MarkChangeCount < 0 {
		return 0, errors.New("invalid clan defaults")
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var found int
	if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM Clan WHERE name=? COLLATE NOCASE LIMIT 1)", cfg.Name).Scan(&found); err != nil {
		return 0, err
	}
	if found != 0 {
		return -1, nil
	}
	if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM ClanMember WHERE userID=? LIMIT 1) OR EXISTS(SELECT 1 FROM ClanMemberRequest WHERE userID=? LIMIT 1)", cfg.MasterID, cfg.MasterID).Scan(&found); err != nil {
		return 0, err
	}
	if found != 0 {
		return -2, nil
	}
	var points int
	err = tx.QueryRow("SELECT points FROM UserCharacter WHERE userID=?", cfg.MasterID).Scan(&points)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if points < ClanCreateCost {
		return -3, nil
	}
	result, err := tx.Exec("UPDATE UserCharacter SET points=points-? WHERE userID=? AND points>=?", ClanCreateCost, cfg.MasterID, ClanCreateCost)
	if err != nil {
		return 0, err
	}
	if n, err := result.RowsAffected(); err != nil {
		return 0, err
	} else if n != 1 {
		return -3, nil
	}
	var clanID int
	if err = tx.QueryRow("SELECT max(coalesce((SELECT clanIDNext FROM UserDist LIMIT 1),1),coalesce((SELECT max(clanID)+1 FROM Clan),1))").Scan(&clanID); err != nil {
		return 0, err
	}
	if _, err = tx.Exec("INSERT INTO Clan(clanID,masterUserID,name,description,notice,gameModeID,mapID,time,memberCount,expBonus,pointBonus,region,joinMethod,score,markID,markColor,markChangeCount,maxMemberCount) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", clanID, cfg.MasterID, cfg.Name, cfg.Description, cfg.Notice, cfg.GameModeID, cfg.MapID, cfg.Time, 1, cfg.ExpBonus, cfg.PointBonus, cfg.Region, cfg.JoinMethod, 0, 0, 0, cfg.MarkChangeCount, cfg.MaxMemberCount); err != nil {
		return 0, err
	}
	if _, err = tx.Exec("INSERT INTO ClanMember(clanID,userID,memberGrade) VALUES(?,?,0)", clanID, cfg.MasterID); err != nil {
		return 0, err
	}
	for page := 0; page < ClanStoragePages; page++ {
		if _, err = tx.Exec("INSERT INTO ClanStoragePage(clanID,pageID,accessGrade) VALUES(?,?,3)", clanID, page); err != nil {
			return 0, err
		}
		for slot := 0; slot < ClanStoragePageSize; slot++ {
			if _, err = tx.Exec("INSERT INTO ClanStorageItem(clanID,pageID,slot,itemID,itemCount,itemDuration,itemEnhValue) VALUES(?,?,?,0,0,0,0)", clanID, page, slot); err != nil {
				return 0, err
			}
		}
	}
	if _, err = tx.Exec("INSERT INTO ClanChronicle(clanID,date,type,string) VALUES(?,?,0,'')", clanID, clanDate(now)); err != nil {
		return 0, err
	}
	if _, err = tx.Exec("UPDATE UserCharacter SET clanID=? WHERE userID=?", clanID, cfg.MasterID); err != nil {
		return 0, err
	}
	if _, err = tx.Exec("UPDATE UserDist SET clanIDNext=? WHERE rowid=(SELECT rowid FROM UserDist LIMIT 1)", clanID+1); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return clanID, nil
}

func (s *Store) clanMembership(tx *sql.Tx, userID int) (int, int, error) {
	var clanID, grade int
	err := tx.QueryRow("SELECT clanID,memberGrade FROM ClanMember WHERE userID=?", userID).Scan(&clanID, &grade)
	return clanID, grade, err
}

func (s *Store) joinClanTx(tx *sql.Tx, clanID, userID, grade int) error {
	if _, err := tx.Exec("INSERT INTO ClanMember(clanID,userID,memberGrade) VALUES(?,?,?)", clanID, userID, grade); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE Clan SET memberCount=(SELECT count(*) FROM ClanMember WHERE clanID=?) WHERE clanID=?", clanID, clanID); err != nil {
		return err
	}
	if _, err := tx.Exec("UPDATE UserCharacter SET clanID=? WHERE userID=?", clanID, userID); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM ClanInvite WHERE destUserID=?", userID); err != nil {
		return err
	}
	_, err := tx.Exec("DELETE FROM ClanMemberRequest WHERE userID=?", userID)
	return err
}

// JoinClan returns the upstream join codes: -1 already a member, -2 duplicate
// pending request, -3 closed, -4 invitation required, -5 request submitted,
// -6 full, -7 pending request for another clan, 0 database error, 1 joined.
func (s *Store) JoinClan(userID, clanID, now int) (int, string, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	var joinMethod, memberCount, maxMember int
	err = tx.QueryRow("SELECT joinMethod,memberCount,maxMemberCount FROM Clan WHERE clanID=?", clanID).Scan(&joinMethod, &memberCount, &maxMember)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	var currentClan int
	err = tx.QueryRow("SELECT clanID FROM ClanMember WHERE userID=?", userID).Scan(&currentClan)
	if err == nil {
		return -1, "", nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, "", err
	}
	if memberCount >= maxMember {
		return -6, "", nil
	}
	var inviter int
	inviteErr := tx.QueryRow("SELECT userID FROM ClanInvite WHERE destUserID=? AND clanID=? ORDER BY rowid DESC LIMIT 1", userID, clanID).Scan(&inviter)
	hasInvite := inviteErr == nil
	if inviteErr != nil && !errors.Is(inviteErr, sql.ErrNoRows) {
		return 0, "", inviteErr
	}
	if joinMethod == 0 {
		return -3, "", nil
	}
	if joinMethod == 1 && !hasInvite {
		return -4, "", nil
	}
	if joinMethod == 2 && !hasInvite {
		var pendingClan int
		err = tx.QueryRow("SELECT clanID FROM ClanMemberRequest WHERE userID=?", userID).Scan(&pendingClan)
		if err == nil {
			if pendingClan == clanID {
				return -2, "", nil
			}
			var name string
			if err = tx.QueryRow("SELECT name FROM Clan WHERE clanID=?", pendingClan).Scan(&name); err != nil {
				return 0, "", err
			}
			return -7, name, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, "", err
		}
		if _, err = tx.Exec("INSERT INTO ClanMemberRequest(clanID,userID,inviterUserID,date) VALUES(?,?,?,?)", clanID, userID, inviter, clanDate(now)); err != nil {
			return 0, "", err
		}
		if err = tx.Commit(); err != nil {
			return 0, "", err
		}
		return -5, "", nil
	}
	if err = s.joinClanTx(tx, clanID, userID, 3); err != nil {
		return 0, "", err
	}
	if err = tx.Commit(); err != nil {
		return 0, "", err
	}
	return 1, "", nil
}

func (s *Store) CancelJoinClan(userID, clanID int) (int, error) {
	result, err := s.DB.Exec("DELETE FROM ClanMemberRequest WHERE userID=? AND clanID=?", userID, clanID)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return -1, nil
	}
	return 1, nil
}

// ApproveClanJoin moves a pending request into ClanMember and returns the new
// member ID so the caller can refresh that member's client.
func (s *Store) ApproveClanJoin(userID int, targetName string, now int) (int, int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	clanID, grade, err := s.clanMembership(tx, userID)
	if errors.Is(err, sql.ErrNoRows) || grade > 1 {
		return -1, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var targetID int
	err = tx.QueryRow("SELECT r.userID FROM ClanMemberRequest r INNER JOIN User u ON u.userID=r.userID WHERE r.clanID=? AND u.userName=? COLLATE NOCASE LIMIT 1", clanID, targetName).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec("DELETE FROM ClanMemberRequest WHERE clanID=? AND userID=?", clanID, targetID); err != nil {
		return 0, 0, err
	}
	if err = s.joinClanTx(tx, clanID, targetID, 3); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec("INSERT INTO ClanChronicle(clanID,date,type,string) VALUES(?,?,2,?)", clanID, clanDate(now), targetName); err != nil {
		return 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return 1, targetID, nil
}

func (s *Store) RejectClanJoin(userID int, targetName string) (int, error) {
	result, err := s.DB.Exec("DELETE FROM ClanMemberRequest WHERE clanID=(SELECT clanID FROM ClanMember WHERE userID=? AND memberGrade<=1 LIMIT 1) AND userID=(SELECT userID FROM User WHERE userName=? COLLATE NOCASE LIMIT 1)", userID, targetName)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return -1, nil
	}
	return 1, nil
}

func (s *Store) RejectAllClanJoins(userID int) (int, error) {
	result, err := s.DB.Exec("DELETE FROM ClanMemberRequest WHERE clanID=(SELECT clanID FROM ClanMember WHERE userID=? AND memberGrade<=1 LIMIT 1)", userID)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return -1, nil
	}
	return 1, nil
}

// LeaveClan refuses to remove the master, matching upstream's memberGrade<>0
// condition.
func (s *Store) LeaveClan(userID int) (int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	clanID, grade, err := s.clanMembership(tx, userID)
	if errors.Is(err, sql.ErrNoRows) || grade == 0 {
		return -1, nil
	}
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec("DELETE FROM ClanMember WHERE clanID=? AND userID=?", clanID, userID); err != nil {
		return 0, err
	}
	if _, err = tx.Exec("UPDATE Clan SET memberCount=(SELECT count(*) FROM ClanMember WHERE clanID=?) WHERE clanID=?", clanID, clanID); err != nil {
		return 0, err
	}
	if _, err = tx.Exec("UPDATE UserCharacter SET clanID=0 WHERE userID=?", userID); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return 1, nil
}

// InviteToClan validates the inviter grade and target character. Online state
// is checked by the server before calling it.
func (s *Store) InviteToClan(userID int, targetName string) (int, int, string, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, 0, "", err
	}
	defer tx.Rollback()
	clanID, grade, err := s.clanMembership(tx, userID)
	if errors.Is(err, sql.ErrNoRows) || grade > 1 {
		return -1, 0, "", nil
	}
	if err != nil {
		return 0, 0, "", err
	}
	var targetID int
	err = tx.QueryRow("SELECT userID FROM UserCharacter WHERE gameName=? COLLATE NOCASE LIMIT 1", targetName).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return -3, 0, "", nil
	}
	if err != nil {
		return 0, 0, "", err
	}
	var targetClan, targetGrade int
	err = tx.QueryRow("SELECT clanID,memberGrade FROM ClanMember WHERE userID=?", targetID).Scan(&targetClan, &targetGrade)
	if err == nil {
		if targetGrade == 0 {
			return -4, 0, "", nil
		}
		if targetClan == clanID {
			return -5, 0, "", nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, "", err
	}
	var inviterName string
	if err = tx.QueryRow("SELECT gameName FROM UserCharacter WHERE userID=?", userID).Scan(&inviterName); err != nil {
		return 0, 0, "", err
	}
	if _, err = tx.Exec("DELETE FROM ClanInvite WHERE destUserID=? AND clanID=?", targetID, clanID); err != nil {
		return 0, 0, "", err
	}
	if _, err = tx.Exec("INSERT INTO ClanInvite(clanID,userID,destUserID) VALUES(?,?,?)", clanID, userID, targetID); err != nil {
		return 0, 0, "", err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, "", err
	}
	return 1, targetID, inviterName, nil
}

func (s *Store) ClanMembers(userID int, adminOnly bool) ([]ClanMember, error) {
	query := "SELECT m.userID,coalesce(u.userName,''),coalesce(c.gameName,''),coalesce(c.level,1),coalesce(c.kills,0),coalesce(c.deaths,0),m.memberGrade FROM ClanMember m INNER JOIN ClanMember self ON self.clanID=m.clanID AND self.userID=? LEFT JOIN UserCharacter c ON c.userID=m.userID LEFT JOIN User u ON u.userID=m.userID WHERE 1=1"
	if adminOnly {
		query += " AND self.memberGrade<=1"
	}
	query += " ORDER BY m.memberGrade,m.userID"
	rows, err := s.DB.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var members []ClanMember
	for rows.Next() {
		var member ClanMember
		if err = rows.Scan(&member.UserID, &member.UserName, &member.GameName, &member.Level, &member.Kills, &member.Deaths, &member.Grade); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func (s *Store) ClanJoinRequests(userID int) ([]ClanJoinRequest, error) {
	rows, err := s.DB.Query("SELECT r.userID,coalesce(u.userName,''),coalesce(c.gameName,''),coalesce(c.level,1),coalesce(c.kills,0),coalesce(c.deaths,0),coalesce((SELECT gameName FROM UserCharacter WHERE userID=r.inviterUserID),''),r.date FROM ClanMemberRequest r INNER JOIN ClanMember self ON self.clanID=r.clanID AND self.userID=? AND self.memberGrade<=1 LEFT JOIN UserCharacter c ON c.userID=r.userID LEFT JOIN User u ON u.userID=r.userID ORDER BY r.date,r.userID", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var requests []ClanJoinRequest
	for rows.Next() {
		var request ClanJoinRequest
		if err = rows.Scan(&request.UserID, &request.UserName, &request.GameName, &request.Level, &request.Kills, &request.Deaths, &request.InviterName, &request.Date); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}
func (s *Store) ClanGrade(userID int) (int, int, error) {
	var clanID, grade int
	err := s.DB.QueryRow("SELECT clanID,memberGrade FROM ClanMember WHERE userID=?", userID).Scan(&clanID, &grade)
	return clanID, grade, err
}

// SetClanGrade lets the master assign officer (1), regular (2) or associate (3)
// grades. Grade 0 is reserved for the single master and is changed through
// DelegateClanMaster instead.
func (s *Store) SetClanGrade(actorID int, targetName string, grade int) (int, int, error) {
	if grade < 1 || grade > 3 {
		return -1, 0, nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	clanID, actorGrade, err := s.clanMembership(tx, actorID)
	if errors.Is(err, sql.ErrNoRows) || actorGrade != 0 {
		return -1, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var targetID int
	err = tx.QueryRow("SELECT m.userID FROM ClanMember m INNER JOIN User u ON u.userID=m.userID WHERE m.clanID=? AND u.userName=? COLLATE NOCASE LIMIT 1", clanID, targetName).Scan(&targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	if targetID == actorID {
		return -1, 0, nil
	}
	if _, err = tx.Exec("UPDATE ClanMember SET memberGrade=? WHERE clanID=? AND userID=?", grade, clanID, targetID); err != nil {
		return 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return 1, targetID, nil
}

// KickClanMember enforces that an actor can only remove members with a lower
// grade than their own, matching upstream's memberGrade comparison.
func (s *Store) KickClanMember(actorID int, targetName string) (int, int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	clanID, actorGrade, err := s.clanMembership(tx, actorID)
	if errors.Is(err, sql.ErrNoRows) || actorGrade > 1 {
		return -1, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var targetID, targetGrade int
	err = tx.QueryRow("SELECT m.userID,m.memberGrade FROM ClanMember m INNER JOIN User u ON u.userID=m.userID WHERE m.clanID=? AND u.userName=? COLLATE NOCASE LIMIT 1", clanID, targetName).Scan(&targetID, &targetGrade)
	if errors.Is(err, sql.ErrNoRows) || targetID == actorID || targetGrade <= actorGrade {
		return -1, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec("DELETE FROM ClanMember WHERE clanID=? AND userID=?", clanID, targetID); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec("UPDATE Clan SET memberCount=(SELECT count(*) FROM ClanMember WHERE clanID=?) WHERE clanID=?", clanID, clanID); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec("UPDATE UserCharacter SET clanID=0 WHERE userID=?", targetID); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec("DELETE FROM ClanMemberRequest WHERE userID=?", targetID); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec("DELETE FROM ClanInvite WHERE destUserID=?", targetID); err != nil {
		return 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return 1, targetID, nil
}

func (s *Store) SetClanJoinMethod(actorID, method int) (int, error) {
	if method < 0 || method > 3 {
		return -1, nil
	}
	result, err := s.DB.Exec("UPDATE Clan SET joinMethod=? WHERE clanID=(SELECT clanID FROM ClanMember WHERE userID=? AND memberGrade<=1 LIMIT 1)", method, actorID)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return -1, nil
	}
	return 1, nil
}

func (s *Store) SetClanRegion(actorID, region int) (int, error) {
	if region < 0 || region > 65535 {
		return -1, nil
	}
	result, err := s.DB.Exec("UPDATE Clan SET region=? WHERE clanID=(SELECT clanID FROM ClanMember WHERE userID=? AND memberGrade<=1 LIMIT 1)", region, actorID)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return -1, nil
	}
	return 1, nil
}

func (s *Store) SetClanNotice(actorID int, notice string) (int, error) {
	result, err := s.DB.Exec("UPDATE Clan SET notice=? WHERE clanID=(SELECT clanID FROM ClanMember WHERE userID=? AND memberGrade<=1 LIMIT 1)", notice, actorID)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return -1, nil
	}
	return 1, nil
}

// SetClanMark consumes one mark-change charge. Result codes: -2 no charge,
// -1 no permission, 1 changed, 2 mark already used by another clan.
func (s *Store) SetClanMark(actorID, markID int) (int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	clanID, grade, err := s.clanMembership(tx, actorID)
	if errors.Is(err, sql.ErrNoRows) || grade > 1 {
		return -1, nil
	}
	if err != nil {
		return 0, err
	}
	var changes, current int
	if err = tx.QueryRow("SELECT markChangeCount,coalesce(markID,0) FROM Clan WHERE clanID=?", clanID).Scan(&changes, &current); err != nil {
		return 0, err
	}
	if changes <= 0 {
		return -2, nil
	}
	if markID != current && markID != 0 {
		var used int
		if err = tx.QueryRow("SELECT EXISTS(SELECT 1 FROM Clan WHERE markID=? AND clanID<>? LIMIT 1)", markID, clanID).Scan(&used); err != nil {
			return 0, err
		}
		if used != 0 {
			return 2, nil
		}
	}
	if _, err = tx.Exec("UPDATE Clan SET markID=?,markChangeCount=markChangeCount-1 WHERE clanID=?", markID, clanID); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return 1, nil
}

// DissolveClan removes the clan and clears every member's character clan ID.
// The returned member IDs let the caller notify online clients.
func (s *Store) DissolveClan(actorID int) (int, []int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, nil, err
	}
	defer tx.Rollback()
	clanID, grade, err := s.clanMembership(tx, actorID)
	if errors.Is(err, sql.ErrNoRows) || grade > 1 {
		return -1, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	rows, err := tx.Query("SELECT userID FROM ClanMember WHERE clanID=?", clanID)
	if err != nil {
		return 0, nil, err
	}
	var members []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return 0, nil, err
		}
		members = append(members, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, nil, err
	}
	if _, err = tx.Exec("UPDATE UserCharacter SET clanID=0 WHERE clanID=?", clanID); err != nil {
		return 0, nil, err
	}
	if _, err = tx.Exec("DELETE FROM Clan WHERE clanID=?", clanID); err != nil {
		return 0, nil, err
	}
	if err = tx.Commit(); err != nil {
		return 0, nil, err
	}
	return 1, members, nil
}

// DelegateClanMaster promotes an officer (grade 1) and demotes the current
// master, matching upstream's two-step update.
func (s *Store) DelegateClanMaster(actorID int, targetName string, now int) (int, int, error) {
	tx, err := s.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	clanID, grade, err := s.clanMembership(tx, actorID)
	if errors.Is(err, sql.ErrNoRows) || grade != 0 {
		return -1, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var targetID, targetGrade int
	err = tx.QueryRow("SELECT m.userID,m.memberGrade FROM ClanMember m INNER JOIN User u ON u.userID=m.userID WHERE m.clanID=? AND u.userName=? COLLATE NOCASE LIMIT 1", clanID, targetName).Scan(&targetID, &targetGrade)
	if errors.Is(err, sql.ErrNoRows) || targetID == actorID || targetGrade != 1 {
		return -1, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec("UPDATE ClanMember SET memberGrade=0 WHERE clanID=? AND userID=?", clanID, targetID); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec("UPDATE ClanMember SET memberGrade=1 WHERE clanID=? AND userID=?", clanID, actorID); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec("INSERT INTO ClanChronicle(clanID,date,type,string) VALUES(?,?,1,?)", clanID, clanDate(now), targetName); err != nil {
		return 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return 1, targetID, nil
}
