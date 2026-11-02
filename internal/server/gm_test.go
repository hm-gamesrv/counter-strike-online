package server

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func startGMTestServer(t *testing.T, gameMaster bool) (string, *store.Store, *Server) {
	t.Helper()
	dir := t.TempDir()
	if e := assets.Init(dir); e != nil {
		t.Fatal(e)
	}
	cfg, e := config.Load(filepath.Join(dir, "ServerConfig.json"))
	if e != nil {
		t.Fatal(e)
	}
	cfg.MaxRegistrationsPerIP = 0
	cfg.DefaultUser.GameMaster = gameMaster
	db, e := store.Open(filepath.Join(dir, "UserDatabase.db3"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	srv, e := New(dir, cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	tcp, udp := listenTestPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, tcp, udp) }()
	t.Cleanup(func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(5 * time.Second):
			t.Error("shutdown hung")
		}
	})
	return tcp.Addr().String(), db, srv
}

func gmHandshake(t *testing.T, addr string, hwid []byte) *testPeer {
	t.Helper()
	conn, e := net.Dial("tcp4", addr)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	g := make([]byte, len(protocol.Greeting))
	if _, e = io.ReadFull(conn, g); e != nil {
		t.Fatal("greeting:", e)
	}
	p := &testPeer{conn, protocol.NewCodec(), t}
	w := &protocol.Writer{}
	w.U8(67)
	w.U16(26)
	w.U32(0)
	w.U32(0)
	p.send(0, w)
	p.next(0)
	w = &protocol.Writer{}
	w.String("steam")
	w.U16(0)
	w.Bytes(hwid)
	w.U32(0)
	w.U32(0)
	w.String("en")
	p.send(3, w)
	return p
}

func gmJoin(t *testing.T, addr, name string, hwid []byte) *testPeer {
	t.Helper()
	p := gmHandshake(t, addr, hwid)
	p.command("/register " + name + " secret1")
	p.next(67)
	p.command("/login " + name + " secret1")
	p.next(1)
	p.next(6)
	w := &protocol.Writer{}
	w.String(name)
	p.send(2, w)
	p.next(150)
	return p
}

func gmReconnect(t *testing.T, addr, name string, hwid []byte) *testPeer {
	t.Helper()
	p := gmHandshake(t, addr, hwid)
	p.command("/login " + name + " secret1")
	p.next(1)
	p.next(150)
	return p
}

// gmNotice returns the text of the next 67 message with the given subtype that
// contains want.
func gmNotice(t *testing.T, p *testPeer, kind byte, want string) string {
	t.Helper()
	for i := 0; i < 500; i++ {
		v := p.next(67)
		r := &protocol.Reader{Data: v.Data}
		if r.U8() != int(kind) {
			continue
		}
		text := r.CString()
		if strings.Contains(text, want) {
			return text
		}
	}
	t.Fatalf("missing notice %q", want)
	return ""
}

// gmNoticeExact requires the notice text to match exactly.
func gmNoticeExact(t *testing.T, p *testPeer, kind byte, want string) {
	t.Helper()
	for i := 0; i < 500; i++ {
		v := p.next(67)
		r := &protocol.Reader{Data: v.Data}
		if r.U8() != int(kind) {
			continue
		}
		if r.CString() == want {
			return
		}
	}
	t.Fatalf("missing exact notice %q", want)
}

func gmSync(t *testing.T, p *testPeer) {
	t.Helper()
	p.command("/version")
	gmNotice(t, p, 11, "CSNZ Go server")
}

func gmExpectClosed(t *testing.T, p *testPeer) {
	t.Helper()
	buf := make([]byte, 256)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		p.conn.SetReadDeadline(deadline)
		if _, e := p.conn.Read(buf); e != nil {
			return
		}
	}
	t.Fatal("connection stayed open")
}

func gmExpectDialClosed(t *testing.T, addr string) {
	t.Helper()
	conn, e := net.Dial("tcp4", addr)
	if e != nil {
		return
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 64)
	if _, e = conn.Read(buf); e == nil {
		t.Fatal("connection accepted during IP ban")
	}
}

func TestGMCommandsRequireGameMaster(t *testing.T) {
	addr, db, _ := startGMTestServer(t, false)
	p := gmJoin(t, addr, "plain", []byte("aaaaaaaaaaaaaaaa"))
	gmJoin(t, addr, "victim", []byte("bbbbbbbbbbbbbbbb"))

	before, err := db.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	p.command("/ban 2 1 cheating 1")
	p.command("/addpoints 100")
	p.command("/register x y")
	gmNotice(t, p, 10, "Already logged in.")

	var bans int
	if err := db.DB.QueryRow("SELECT count(*) FROM UserBan").Scan(&bans); err != nil || bans != 0 {
		t.Fatalf("non-GM /ban executed: %d %v", bans, err)
	}
	character, err := db.Character(1)
	if err != nil || character.Points != before.Points {
		t.Fatalf("non-GM /addpoints executed: %+v %v", character, err)
	}
}

func TestGMBanCommandsAndEnforcement(t *testing.T) {
	addr, db, _ := startGMTestServer(t, true)
	adminHWID := []byte("aaaaaaaaaaaaaaaa")
	victimHWID := []byte("bbbbbbbbbbbbbbbb")
	admin := gmJoin(t, addr, "admin", adminHWID)
	victim := gmJoin(t, addr, "victim", victimHWID)

	admin.command("/ban 2 1 cheating 1")
	gmNotice(t, admin, 11, "victim is banned. Reason: cheating")
	gmExpectClosed(t, victim)

	var banType, term int
	var reason string
	if err := db.DB.QueryRow("SELECT type,reason,term FROM UserBan WHERE userID=2").Scan(&banType, &reason, &term); err != nil {
		t.Fatal(err)
	}
	if banType != 1 || reason != "cheating" || term < int(time.Now().Unix()/60) {
		t.Fatalf("stored ban %d %q %d", banType, reason, term)
	}

	blocked := gmHandshake(t, addr, victimHWID)
	blocked.command("/login victim secret1")
	gmNotice(t, blocked, 10, "Account is banned.")

	admin.command("/unban 2")
	admin.command("/getuid victim")
	gmNoticeExact(t, admin, 11, "2")

	victim = gmReconnect(t, addr, "victim", victimHWID)

	admin.command("/hban 2")
	gmSync(t, admin)
	gmExpectClosed(t, victim)

	bannedHWID := gmHandshake(t, addr, victimHWID)
	gmExpectClosed(t, bannedHWID)

	admin.command("/unban 2")
	gmSync(t, admin)
	victim = gmReconnect(t, addr, "victim", victimHWID)

	admin.command("/ipban 2")
	gmSync(t, admin)
	gmExpectClosed(t, victim)
	gmExpectDialClosed(t, addr)

	admin.command("/unban 2")
	gmSync(t, admin)
	victim = gmReconnect(t, addr, "victim", victimHWID)
	victim.command("/register x y")
	gmNotice(t, victim, 10, "Already logged in.")
}

func gmItemCount(t *testing.T, db *store.Store, userID, itemID int) int {
	t.Helper()
	items, err := db.Inventory(userID)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, item := range items {
		if item.ID == itemID {
			total += item.Count
		}
	}
	return total
}

func TestGMItemExpAndRewardCommands(t *testing.T) {
	addr, db, _ := startGMTestServer(t, true)
	admin := gmJoin(t, addr, "admin", []byte("aaaaaaaaaaaaaaaa"))
	gmJoin(t, addr, "victim", []byte("bbbbbbbbbbbbbbbb"))

	base := gmItemCount(t, db, 1, 567)
	admin.command("/additem 567 2 0")
	gmNotice(t, admin, 11, "Item 567 x2 added.")
	afterAdd := gmItemCount(t, db, 1, 567)
	if afterAdd != base+2 {
		t.Fatalf("additem count %d, want %d", afterAdd, base+2)
	}

	admin.command("/removeitem 567")
	gmNotice(t, admin, 11, "Select one of the available item slots to remove:")
	admin.command("/removeitem 567 0")
	gmNotice(t, admin, 11, "Item (567, 0) removed")
	if got := gmItemCount(t, db, 1, 567); got >= afterAdd {
		t.Fatalf("removeitem count %d, want below %d", got, afterAdd)
	}

	admin.command("/addexp 500")
	gmSync(t, admin)
	character, err := db.Character(1)
	if err != nil {
		t.Fatal(err)
	}
	if character.Exp != 500 {
		t.Fatalf("addexp stored %d", character.Exp)
	}

	admin.command("/givereward 2 2189")
	admin.command("/getuid victim")
	gmNoticeExact(t, admin, 11, "2")
	if got := gmItemCount(t, db, 2, 8084); got < 1 {
		t.Fatalf("givereward inventory %d", got)
	}

	admin.command("/giverewardtoall 2189")
	gmNotice(t, admin, 11, "sent to")
	if got := gmItemCount(t, db, 1, 8084); got < 1 {
		t.Fatalf("giverewardtoall inventory %d", got)
	}
	admin.command("/givereward 2 999999")
	gmNotice(t, admin, 11, "Reward does not exist")
}

func TestGMSetItemFieldsAndAllItems(t *testing.T) {
	addr, db, _ := startGMTestServer(t, true)
	admin := gmJoin(t, addr, "admin", []byte("aaaaaaaaaaaaaaaa"))

	items, err := db.Inventory(1)
	if err != nil || len(items) == 0 {
		t.Fatalf("inventory %v %v", items, err)
	}
	slot := items[0].Slot
	admin.command(fmt.Sprintf("/setitemstatus %d 3", slot))
	admin.command("/register x y")
	gmNotice(t, admin, 10, "Already logged in.")
	var status int
	if err = db.DB.QueryRow("SELECT status FROM UserInventory WHERE userID=1 AND slot=?", slot).Scan(&status); err != nil || status != 3 {
		t.Fatalf("status %d %v", status, err)
	}
	admin.command(fmt.Sprintf("/setiteminuse %d 1", slot))
	admin.command("/register x y")
	gmNotice(t, admin, 10, "Already logged in.")
	var inUse int
	if err = db.DB.QueryRow("SELECT inUse FROM UserInventory WHERE userID=1 AND slot=?", slot).Scan(&inUse); err != nil || inUse != 1 {
		t.Fatalf("inUse %d %v", inUse, err)
	}

	var before int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1").Scan(&before); err != nil {
		t.Fatal(err)
	}
	admin.command("/addallitems")
	gmNotice(t, admin, 11, "Done")
	var after int
	if err = db.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after <= before+100 {
		t.Fatalf("addallitems inventory %d -> %d", before, after)
	}
}

func TestGMAddWeaponReleaseCharacter(t *testing.T) {
	addr, db, _ := startGMTestServer(t, true)
	admin := gmJoin(t, addr, "admin", []byte("aaaaaaaaaaaaaaaa"))
	admin.command("/addwpnreleasechar B 4")
	admin.command("/register x y")
	gmNotice(t, admin, 10, "Already logged in.")
	letters, total, err := db.WeaponReleaseCharacters(1)
	if err != nil || total != 4 || len(letters) != 1 || letters[0].Character != 'B' || letters[0].Count != 4 {
		t.Fatalf("letters %+v total %d, %v", letters, total, err)
	}
}
