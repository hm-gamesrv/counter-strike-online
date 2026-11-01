package store

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

type Store struct{ DB *sql.DB }
type User struct {
	ID   int
	Name string
}
type Character struct {
	ID                                                                                                                      int
	Name                                                                                                                    string
	Level, Exp, Points, Cash, Battles, Win, Kills, Deaths, PasswordBoxes, MileagePoints, HonorPoints, PrefixID, ChatColorID int
}
type Item struct {
	Slot, ID, Count, Status, InUse, Obtain, Expiry, Level, Exp, Value, Paint, Part1, Part2, Lock int
	Paints                                                                                       []int
}

var ErrCredentials = errors.New("Wrong password or username.")
var validName = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

const MaxUsernameLength = 15
const MaxCharacterNameLength = 16

func Open(path string) (*Store, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	s := &Store{db}
	if e = s.init(); e != nil {
		db.Close()
		return nil, e
	}
	if e = s.initRewardClaims(); e != nil {
		db.Close()
		return nil, e
	}
	return s, nil
}
func (s *Store) init() error {
	if _, e := s.DB.Exec("PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL;"); e != nil {
		return e
	}
	var ver int
	if e := s.DB.QueryRow("PRAGMA user_version").Scan(&ver); e != nil {
		return e
	}
	if ver == 0 {
		var count int
		if e := s.DB.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return errors.New("unversioned nonempty database: refusing to initialize")
		}
		tx, e := s.DB.Begin()
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if _, e = tx.Exec(schema); e != nil {
			return e
		}
		return tx.Commit()
	}
	if ver != 4 {
		return fmt.Errorf("database version %d unsupported (expected v4)", ver)
	}
	return nil
}
func (s *Store) Close() error { return s.DB.Close() }
func hashPassword(p string) (string, error) {
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key, e := pbkdf2.Key(sha256.New, p, salt, 120000, 32)
	return "pbkdf2-sha256$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(key), e
}
func checkPassword(p, stored string) bool {
	a := strings.Split(stored, "$")
	if len(a) != 3 || a[0] != "pbkdf2-sha256" {
		return false
	}
	salt, e := hex.DecodeString(a[1])
	if e != nil || len(salt) != 16 {
		return false
	}
	want, e := hex.DecodeString(a[2])
	if e != nil || len(want) != 32 {
		return false
	}
	got, e := pbkdf2.Key(sha256.New, p, salt, 120000, 32)
	return e == nil && subtle.ConstantTimeCompare(got, want) == 1
}
func (s *Store) Register(name, password, ip string, limit int) error {
	if len(name) < 5 || len(name) > MaxUsernameLength || !validName.MatchString(name) {
		return errors.New("Username must be 5..15 English letters or digits.")
	}
	if len(password) < 5 || len(password) > 15 || strings.Trim(password, "0123456789") == "" {
		return errors.New("Password must be 5..15 characters and not only numbers.")
	}
	h, e := hashPassword(password)
	if e != nil {
		return e
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRow("SELECT count(*) FROM User WHERE userName=? COLLATE NOCASE", name).Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return errors.New("User with this username already exists.")
	}
	if limit > 0 {
		if e = tx.QueryRow("SELECT count(*) FROM User WHERE registerIP=?", ip).Scan(&n); e != nil {
			return e
		}
		if n >= limit {
			return errors.New("You have exceeded the account limit for one IP.")
		}
	}
	var id int
	if e = tx.QueryRow("SELECT max(coalesce((SELECT max(userID)+1 FROM User),1),coalesce((SELECT userIDNext FROM UserDist LIMIT 1),1))").Scan(&id); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO User(userID,userName,password,registerTime,registerIP) VALUES(?,?,?,?,?)", id, name, h, nowMinutes(), ip); e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE UserDist SET userIDNext=?", id+1); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) Login(name, password, ip string, hwid []byte) (User, error) {
	var u User
	var h string
	e := s.DB.QueryRow("SELECT userID,userName,password FROM User WHERE userName=?", name).Scan(&u.ID, &u.Name, &h)
	if errors.Is(e, sql.ErrNoRows) {
		return u, ErrCredentials
	}
	if e != nil {
		return u, e
	}
	if !checkPassword(password, h) {
		return u, ErrCredentials
	}
	var banned int
	e = s.DB.QueryRow("SELECT count(*) FROM UserBan WHERE userID=? AND (term=0 OR term>?)", u.ID, nowMinutes()).Scan(&banned)
	if e != nil {
		return u, e
	}
	if banned != 0 {
		return u, errors.New("Account is banned.")
	}
	_, e = s.DB.Exec("UPDATE User SET firstLogonTime=CASE WHEN firstLogonTime=0 THEN ? ELSE firstLogonTime END,lastLogonTime=?,lastIP=?,lastHWID=? WHERE userID=?", nowMinutes(), nowMinutes(), ip, hwid, u.ID)
	return u, e
}
func (s *Store) Character(id int) (Character, error) {
	c := Character{ID: id}
	e := s.DB.QueryRow("SELECT gameName,level,exp,points,cash,battles,win,kills,deaths,passwordBoxes,mileagePoints,honorPoints,prefixID,chatColorID FROM UserCharacter WHERE userID=?", id).Scan(&c.Name, &c.Level, &c.Exp, &c.Points, &c.Cash, &c.Battles, &c.Win, &c.Kills, &c.Deaths, &c.PasswordBoxes, &c.MileagePoints, &c.HonorPoints, &c.PrefixID, &c.ChatColorID)
	return c, e
}
func (s *Store) CreateCharacter(id int, name string, d config.DefaultUser) error {
	if len(name) < 4 || len(name) > MaxCharacterNameLength || !validName.MatchString(name) {
		return errors.New("Character name must be 4..16 English letters or digits.")
	}
	tx, e := s.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRow("SELECT count(*) FROM UserCharacter WHERE gameName=? COLLATE NOCASE", name).Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return errors.New("Character name already exists.")
	}
	_, e = tx.Exec("INSERT INTO UserCharacter(userID,gameName,level,exp,points,battles,win,kills,deaths,passwordBoxes,mileagePoints,honorPoints,prefixID) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)", id, name, d.Level, d.Exp, d.Points, d.Battles, d.Win, d.Kills, d.Deaths, d.PasswordBoxes, d.MileagePoints, d.HonorPoints, d.PrefixID)
	if e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO UserCharacterExtended(userID,gameMaster,nextInventorySlot) VALUES(?,?,?)", id, d.GameMaster, len(d.PseudoDefaultItems)+2); e != nil {
		return e
	}
	for i := 0; i < 12; i++ {
		a := [4]int{12, 2, 161, 31}
		for j := range a {
			if v, ok := d.Loadouts[strconv.Itoa(i)][strconv.Itoa(j+1)]; ok {
				a[j] = v
			}
		}
		if _, e = tx.Exec("INSERT INTO UserLoadout VALUES(?,?,?,?,?,?)", id, i, a[0], a[1], a[2], a[3]); e != nil {
			return e
		}
	}
	for i := 0; i < 17; i++ {
		a := make([]any, 11)
		a[0] = id
		a[1] = i
		for j := 0; j < 9; j++ {
			a[j+2] = d.BuyMenu[strconv.Itoa(i)][strconv.Itoa(j)]
		}
		if _, e = tx.Exec("INSERT INTO UserBuyMenu VALUES(?,?,?,?,?,?,?,?,?,?,?)", a...); e != nil {
			return e
		}
	}
	for i := 0; i < 8; i++ {
		if _, e = tx.Exec("INSERT INTO UserBookmark VALUES(?,?,0)", id, i); e != nil {
			return e
		}
	}
	items := append(append([]int{}, d.PseudoDefaultItems...), 1001)
	for i, item := range items {
		lock := 2
		status := 1
		if item == 1001 {
			lock = 0
			status = 0
		}
		if _, e = tx.Exec("INSERT INTO UserInventory(userID,slot,itemID,count,status,inUse,obtainDate,expiryDate,isClanItem,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus) VALUES(?,?,?,1,?,1,?,0,0,0,0,0,0,0,0,?)", id, i+1, item, status, nowMinutes(), lock); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) Inventory(id int) ([]Item, error) {
	return readInventory(s.DB, id)
}

type inventoryQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
}

func readInventory(q inventoryQuerier, id int) ([]Item, error) {
	rows, e := q.Query("SELECT slot,itemID,count,status,inUse,obtainDate,expiryDate,enhancementLevel,enhancementExp,enhanceValue,paintID,partSlot1,partSlot2,lockStatus,paintIDList FROM UserInventory WHERE userID=? AND count>0 AND (expiryDate=0 OR expiryDate>? OR (inUse=0 AND status=0 AND expiryDate BETWEEN 1 AND 36500)) ORDER BY slot", id, nowMinutes())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var i Item
		var paints string
		if e = rows.Scan(&i.Slot, &i.ID, &i.Count, &i.Status, &i.InUse, &i.Obtain, &i.Expiry, &i.Level, &i.Exp, &i.Value, &i.Paint, &i.Part1, &i.Part2, &i.Lock, &paints); e != nil {
			return nil, e
		}
		for _, v := range strings.Split(paints, ",") {
			if n, _ := strconv.Atoi(v); n != 0 {
				i.Paints = append(i.Paints, n)
			}
		}
		items = append(items, i)
	}
	return items, rows.Err()
}
