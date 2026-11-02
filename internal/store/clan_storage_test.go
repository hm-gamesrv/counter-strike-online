package store

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func clanStorageUser(t *testing.T, s *Store, name string, points int) int {
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

func addClanInventoryItem(t *testing.T, s *Store, userID, slot, itemID, expiry int) {
	t.Helper()
	if _, err := s.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,?,?,1,1,0,?,?,0,0,0,0,0,0,0,0)", userID, slot, itemID, nowMinutes(), expiry); err != nil {
		t.Fatal(err)
	}
}

func TestClanStorageGiveTakeDeleteAndAccess(t *testing.T) {
	s := testStore(t)
	master := clanStorageUser(t, s, "master", 40000)
	target := clanStorageUser(t, s, "target", 0)
	clanID, err := s.CreateClan(ClanCreate{MasterID: master, Name: "Alpha", JoinMethod: 3}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, err := s.JoinClan(target, clanID, 1000); err != nil || code != 1 {
		t.Fatalf("join %d, %v", code, err)
	}
	addClanInventoryItem(t, s, master, 40, 270, 0)
	changed, code, err := s.GiveClanItem(master, 0, 40)
	if err != nil || code != 1 || len(changed) != 1 || changed[0].Slot != 40 || changed[0].ID != 0 {
		t.Fatalf("give item %+v %d, %v", changed, code, err)
	}
	var count int
	if err = s.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=? AND slot=40", master).Scan(&count); err != nil || count != 0 {
		t.Fatalf("inventory after give %d, %v", count, err)
	}
	items, err := s.ClanStoragePage(master, 0)
	if err != nil || len(items) != 1 || items[0].ItemID != 270 || items[0].Slot != 0 {
		t.Fatalf("storage page %+v, %v", items, err)
	}
	changed, code, err = s.TakeClanItem(target, 0, 0, 3000, 0)
	if err != nil || code != 1 || len(changed) != 1 || changed[0].ID != 270 {
		t.Fatalf("take item %+v %d, %v", changed, code, err)
	}
	if err = s.DB.QueryRow("SELECT count(*) FROM ClanStorageItem WHERE clanID=? AND pageID=0 AND itemID<>0", clanID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("storage after take %d, %v", count, err)
	}
	if code, err = s.DeleteClanItem(target, 0, 0); err != nil || code != -1 {
		t.Fatalf("member delete %d, %v", code, err)
	}
	addClanInventoryItem(t, s, master, 41, 271, 0)
	if _, code, err = s.GiveClanItem(master, 0, 41); err != nil || code != 1 {
		t.Fatalf("second give %d, %v", code, err)
	}
	if code, err = s.DeleteClanItem(master, 0, 0); err != nil || code != 1 {
		t.Fatalf("master delete %d, %v", code, err)
	}
	if code, err = s.SetClanStorageAccess(master, 0, 1); err != nil || code != 1 {
		t.Fatalf("access update %d, %v", code, err)
	}
	addClanInventoryItem(t, s, target, 42, 272, 0)
	if _, code, err = s.GiveClanItem(target, 0, 42); err != nil || code != -1 {
		t.Fatalf("restricted give %d, %v", code, err)
	}
}

func TestClanStorageFullAndDuplicateChecks(t *testing.T) {
	s := testStore(t)
	master := clanStorageUser(t, s, "master", 40000)
	target := clanStorageUser(t, s, "target", 0)
	clanID, err := s.CreateClan(ClanCreate{MasterID: master, Name: "Alpha", JoinMethod: 3}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if code, _, err := s.JoinClan(target, clanID, 1000); err != nil || code != 1 {
		t.Fatalf("join %d, %v", code, err)
	}
	if _, err = s.DB.Exec("UPDATE ClanStorageItem SET itemID=1,itemCount=1 WHERE clanID=? AND pageID=1", clanID); err != nil {
		t.Fatal(err)
	}
	addClanInventoryItem(t, s, master, 50, 270, 0)
	if _, code, err := s.GiveClanItem(master, 1, 50); err != nil || code != -3 {
		t.Fatalf("full page %d, %v", code, err)
	}
	addClanInventoryItem(t, s, target, 51, 271, 0)
	if _, code, err := s.GiveClanItem(target, 0, 51); err != nil || code != 1 {
		t.Fatalf("target give %d, %v", code, err)
	}
	addClanInventoryItem(t, s, target, 52, 271, 0)
	if _, code, err := s.TakeClanItem(target, 0, 0, 3000, 0); err != nil || code != -3 {
		t.Fatalf("duplicate permanent %d, %v", code, err)
	}
	addClanInventoryItem(t, s, master, 53, 273, 0)
	if _, code, err := s.GiveClanItem(master, 0, 53); err != nil || code != 1 {
		t.Fatalf("master give %d, %v", code, err)
	}
	if _, code, err := s.TakeClanItem(target, 0, 1, 3000, 0); err != nil || code != 1 {
		t.Fatalf("target take %d, %v", code, err)
	}
	addClanInventoryItem(t, s, master, 54, 274, 0)
	if _, code, err := s.GiveClanItem(master, 0, 54); err != nil || code != 1 {
		t.Fatalf("give clan duplicate %d, %v", code, err)
	}
	if _, err = s.DB.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,60,274,1,1,0,?,0,1,0,0,0,0,0,0,0)", target, nowMinutes()); err != nil {
		t.Fatal(err)
	}
	if _, code, err := s.TakeClanItem(target, 0, 1, 3000, 0); err != nil || code != -5 {
		t.Fatalf("duplicate clan item %d, %v", code, err)
	}
}
