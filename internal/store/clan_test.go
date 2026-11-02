package store

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func clanUser(t *testing.T, s *Store, name string, points int) int {
	t.Helper()
	if err := s.Register(name, "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	var id int
	if err := s.DB.QueryRow("SELECT userID FROM User WHERE userName=?", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(id, name, config.DefaultUser{Level: 1, Points: points}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCreateClanDeductsPointsAndBuildsStorage(t *testing.T) {
	s := testStore(t)
	master := clanUser(t, s, "master", 40000)
	clanID, err := s.CreateClan(ClanCreate{
		MasterID:        master,
		Name:            "Alpha",
		Description:     "first",
		Notice:          "hello",
		GameModeID:      4,
		MapID:           277,
		Region:          3,
		JoinMethod:      3,
		ExpBonus:        5,
		PointBonus:      5,
		MarkChangeCount: 1,
	}, 1000)
	if err != nil || clanID != 1 {
		t.Fatalf("create clan %d, %v", clanID, err)
	}
	character, err := s.Character(master)
	if err != nil || character.Points != 10000 || character.ClanID != clanID {
		t.Fatalf("character after create %+v, %v", character, err)
	}
	var pages, slots int
	if err = s.DB.QueryRow("SELECT count(*) FROM ClanStoragePage WHERE clanID=?", clanID).Scan(&pages); err != nil || pages != ClanStoragePages {
		t.Fatalf("storage pages %d, %v", pages, err)
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM ClanStorageItem WHERE clanID=?", clanID).Scan(&slots); err != nil || slots != ClanStoragePages*ClanStoragePageSize {
		t.Fatalf("storage slots %d, %v", slots, err)
	}
	clan, err := s.ClanInfo(clanID)
	if err != nil || clan.Name != "Alpha" || clan.MasterName != "master" || clan.MemberCount != 1 || clan.MarkChangeCount != 1 {
		t.Fatalf("clan info %+v, %v", clan, err)
	}
	if code, err := s.CreateClan(ClanCreate{MasterID: master, Name: "Alpha"}, 1000); err != nil || code != -1 {
		t.Fatalf("duplicate create code %d, %v", code, err)
	}
	second := clanUser(t, s, "second", 40000)
	if code, err := s.CreateClan(ClanCreate{MasterID: second, Name: "Beta", JoinMethod: 3}, 1000); err != nil || code != 2 {
		t.Fatalf("second clan %d, %v", code, err)
	}
	third := clanUser(t, s, "third", 100)
	if code, err := s.CreateClan(ClanCreate{MasterID: third, Name: "Gamma", JoinMethod: 3}, 1000); err != nil || code != -3 {
		t.Fatalf("poor clan %d, %v", code, err)
	}
}

func TestClanJoinApprovalMemberListAndLeave(t *testing.T) {
	s := testStore(t)
	master := clanUser(t, s, "master", 40000)
	target := clanUser(t, s, "target", 0)
	clanID, err := s.CreateClan(ClanCreate{MasterID: master, Name: "Alpha", JoinMethod: 2}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	code, _, err := s.JoinClan(target, clanID, 1001)
	if err != nil || code != -5 {
		t.Fatalf("join request %d, %v", code, err)
	}
	requests, err := s.ClanJoinRequests(master)
	if err != nil || len(requests) != 1 || requests[0].UserName != "target" {
		t.Fatalf("join requests %+v, %v", requests, err)
	}
	code, joined, err := s.ApproveClanJoin(master, "target", 1002)
	if err != nil || code != 1 || joined != target {
		t.Fatalf("approve %d %d, %v", code, joined, err)
	}
	character, err := s.Character(target)
	if err != nil || character.ClanID != clanID {
		t.Fatalf("target clan %d, %v", character.ClanID, err)
	}
	members, err := s.ClanMembers(master, true)
	if err != nil || len(members) != 2 || members[0].Grade != 0 || members[1].Grade != 3 {
		t.Fatalf("members %+v, %v", members, err)
	}
	if members, err = s.ClanMembers(target, true); err != nil || len(members) != 0 {
		t.Fatalf("non-admin members %+v, %v", members, err)
	}
	if code, err = s.LeaveClan(master); err != nil || code != -1 {
		t.Fatalf("master leave %d, %v", code, err)
	}
	if code, err = s.LeaveClan(target); err != nil || code != 1 {
		t.Fatalf("member leave %d, %v", code, err)
	}
	clan, err := s.ClanInfo(clanID)
	if err != nil || clan.MemberCount != 1 {
		t.Fatalf("member count %+v, %v", clan, err)
	}
}

func TestClanInviteOnlyAndDirectJoin(t *testing.T) {
	s := testStore(t)
	master := clanUser(t, s, "master", 40000)
	target := clanUser(t, s, "target", 0)
	clanID, err := s.CreateClan(ClanCreate{MasterID: master, Name: "Alpha", JoinMethod: 1}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, err := s.JoinClan(target, clanID, 1001); err != nil || code != -4 {
		t.Fatalf("join without invite %d, %v", code, err)
	}
	code, invited, inviter, err := s.InviteToClan(master, "target")
	if err != nil || code != 1 || invited != target || inviter != "master" {
		t.Fatalf("invite %d %d %q, %v", code, invited, inviter, err)
	}
	if code, _, err = s.JoinClan(target, clanID, 1002); err != nil || code != 1 {
		t.Fatalf("invited join %d, %v", code, err)
	}
	if code, _, _, err = s.InviteToClan(master, "target"); err != nil || code != -5 {
		t.Fatalf("same clan invite %d, %v", code, err)
	}
	if code, _, _, err = s.InviteToClan(target, "other"); err != nil || code != -1 {
		t.Fatalf("member invite %d, %v", code, err)
	}
	if code, _, _, err = s.InviteToClan(master, "missing"); err != nil || code != -3 {
		t.Fatalf("missing invite %d, %v", code, err)
	}
}

func TestClanListFilters(t *testing.T) {
	s := testStore(t)
	master := clanUser(t, s, "master", 40000)
	if _, err := s.CreateClan(ClanCreate{MasterID: master, Name: "Alpha", GameModeID: 4, Time: 2, JoinMethod: 3}, 1000); err != nil {
		t.Fatal(err)
	}
	second := clanUser(t, s, "second", 40000)
	if _, err := s.CreateClan(ClanCreate{MasterID: second, Name: "Beta", GameModeID: 7, Time: 1, JoinMethod: 3}, 1000); err != nil {
		t.Fatal(err)
	}
	all, pages, err := s.ClanList("", 0, 0, 0, 0)
	if err != nil || len(all) != 2 || pages != 1 {
		t.Fatalf("all clans %+v pages %d, %v", all, pages, err)
	}
	byName, _, err := s.ClanList("Alp", 0, 0, 0, 0)
	if err != nil || len(byName) != 1 || byName[0].Name != "Alpha" {
		t.Fatalf("name filter %+v, %v", byName, err)
	}
	byMode, _, err := s.ClanList("", 1, 7, 0, 0)
	if err != nil || len(byMode) != 1 || byMode[0].Name != "Beta" {
		t.Fatalf("mode filter %+v, %v", byMode, err)
	}
	byTime, _, err := s.ClanList("", 2, 0, 2, 0)
	if err != nil || len(byTime) != 1 || byTime[0].Name != "Alpha" {
		t.Fatalf("time filter %+v, %v", byTime, err)
	}
	byBoth, _, err := s.ClanList("", 3, 4, 2, 0)
	if err != nil || len(byBoth) != 1 || byBoth[0].Name != "Alpha" {
		t.Fatalf("mode and time filter %+v, %v", byBoth, err)
	}
}
func TestClanManagementRules(t *testing.T) {
	s := testStore(t)
	master := clanUser(t, s, "master", 40000)
	target := clanUser(t, s, "target", 0)
	other := clanUser(t, s, "other", 0)
	clanID, err := s.CreateClan(ClanCreate{MasterID: master, Name: "Alpha", JoinMethod: 3, MarkChangeCount: 1}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{target, other} {
		if code, _, err := s.JoinClan(id, clanID, 1000); err != nil || code != 1 {
			t.Fatalf("join %d: %d, %v", id, code, err)
		}
	}
	if code, got, err := s.SetClanGrade(master, "target", 1); err != nil || code != 1 || got != target {
		t.Fatalf("set grade %d %d, %v", code, got, err)
	}
	if code, _, err := s.SetClanGrade(target, "other", 1); err != nil || code != -1 {
		t.Fatalf("officer set grade %d, %v", code, err)
	}
	if code, err := s.SetClanJoinMethod(target, 2); err != nil || code != 1 {
		t.Fatalf("officer config %d, %v", code, err)
	}
	if code, err := s.SetClanRegion(target, 7); err != nil || code != 1 {
		t.Fatalf("officer region %d, %v", code, err)
	}
	if code, err := s.SetClanNotice(target, "notice"); err != nil || code != 1 {
		t.Fatalf("officer notice %d, %v", code, err)
	}
	if code, err := s.SetClanMark(master, 9); err != nil || code != 1 {
		t.Fatalf("mark %d, %v", code, err)
	}
	if code, err := s.SetClanMark(master, 10); err != nil || code != -2 {
		t.Fatalf("second mark %d, %v", code, err)
	}
	if code, targetID, err := s.KickClanMember(master, "other"); err != nil || code != 1 || targetID != other {
		t.Fatalf("kick %d %d, %v", code, targetID, err)
	}
	if code, _, err := s.KickClanMember(target, "master"); err != nil || code != -1 {
		t.Fatalf("officer kick master %d, %v", code, err)
	}
	if code, targetID, err := s.DelegateClanMaster(master, "target", 1001); err != nil || code != 1 || targetID != target {
		t.Fatalf("delegate %d %d, %v", code, targetID, err)
	}
	_, targetGrade, err := s.ClanGrade(target)
	if err != nil || targetGrade != 0 {
		t.Fatalf("target grade %d, %v", targetGrade, err)
	}
	_, masterGrade, err := s.ClanGrade(master)
	if err != nil || masterGrade != 1 {
		t.Fatalf("master grade %d, %v", masterGrade, err)
	}
	if code, members, err := s.DissolveClan(target); err != nil || code != 1 || len(members) != 2 {
		t.Fatalf("dissolve %d %v, %v", code, members, err)
	}
	if _, err := s.ClanInfo(clanID); err == nil {
		t.Fatal("clan still exists after dissolve")
	}
	for _, id := range []int{master, target} {
		character, err := s.Character(id)
		if err != nil || character.ClanID != 0 {
			t.Fatalf("character %d clan %d, %v", id, character.ClanID, err)
		}
	}
}
