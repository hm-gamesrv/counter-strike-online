package store

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
)

func weaponReleaseUser(t *testing.T, s *Store, letters map[byte]int) int {
	t.Helper()
	if err := s.Register("release", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	var id int
	if err := s.DB.QueryRow("SELECT userID FROM User WHERE userName='release'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(id, "Release", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	for letter, count := range letters {
		if _, err := s.DB.Exec("INSERT INTO UserMiniGameWeaponReleaseCharacters(userID,character,count) VALUES(?,?,?)", id, string(letter), count); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestWeaponReleaseProgressAndLetters(t *testing.T) {
	s := testStore(t)
	user := weaponReleaseUser(t, s, map[byte]int{'G': 1, 'A': 1, 'L': 2, 'I': 1})
	bits := []struct {
		bit    int
		letter byte
	}{
		{4, 'L'},
		{3, 'I'},
		{2, 'L'},
		{1, 'A'},
		{0, 'G'},
	}
	want := 0
	for i, step := range bits {
		code, progress, left, err := s.SetWeaponReleaseCharacter(user, 567, step.bit, int(step.letter), i == len(bits)-1)
		if err != nil || code != 1 {
			t.Fatalf("set %d: %d, %v", i, code, err)
		}
		want |= 1 << step.bit
		if progress != want {
			t.Fatalf("progress %d after %d, want %d", progress, i, want)
		}
		if left < 0 {
			t.Fatalf("left %d", left)
		}
	}
	rows, err := s.WeaponReleaseRows(user)
	if err != nil || len(rows) != 1 || rows[0].ItemID != 567 || rows[0].Progress != 0x1F || rows[0].Opened != 1 {
		t.Fatalf("rows %+v, %v", rows, err)
	}
	if code, _, _, err := s.SetWeaponReleaseCharacter(user, 567, 0, int('G'), true); err != nil || code != -1 {
		t.Fatalf("opened row %d, %v", code, err)
	}
	if code, _, _, err := s.SetWeaponReleaseCharacter(user, 568, 0, int('X'), false); err != nil || code != -3 {
		t.Fatalf("missing letter %d, %v", code, err)
	}
}

func TestWeaponReleaseDuplicateBit(t *testing.T) {
	s := testStore(t)
	user := weaponReleaseUser(t, s, map[byte]int{'A': 2})
	if code, _, _, err := s.SetWeaponReleaseCharacter(user, 567, 0, int('A'), false); err != nil || code != 1 {
		t.Fatalf("first bit %d, %v", code, err)
	}
	if code, _, _, err := s.SetWeaponReleaseCharacter(user, 567, 0, int('A'), false); err != nil || code != -2 {
		t.Fatalf("duplicate bit %d, %v", code, err)
	}
	characters, total, err := s.WeaponReleaseCharacters(user)
	if err != nil || total != 1 || len(characters) != 1 || characters[0].Count != 1 {
		t.Fatalf("characters %+v total %d, %v", characters, total, err)
	}
}
