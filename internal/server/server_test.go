package server

import (
	"context"
	"crypto/rc4"
	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"
)

type testPeer struct {
	conn  net.Conn
	codec *protocol.Codec
	t     *testing.T
}

func (p *testPeer) send(id byte, w *protocol.Writer) {
	p.t.Helper()
	if e := p.codec.Write(p.conn, id, w.Data); e != nil {
		p.t.Fatal(e)
	}
}
func (p *testPeer) next(id byte) protocol.Packet {
	p.t.Helper()
	for i := 0; i < 200; i++ {
		p.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		v, e := p.codec.Read(p.conn)
		if e != nil {
			p.t.Fatal("waiting for", id, e)
		}
		if v.ID == id {
			return v
		}
	}
	p.t.Fatal("missing packet", id)
	return protocol.Packet{}
}
func (p *testPeer) command(text string) {
	w := &protocol.Writer{}
	w.U8(1)
	w.String(text)
	p.send(67, w)
}
func (p *testPeer) roomPacket(kind int) protocol.Packet {
	p.t.Helper()
	for i := 0; i < 200; i++ {
		v := p.next(65)
		if len(v.Data) > 0 && int(v.Data[0]) == kind {
			return v
		}
	}
	p.t.Fatal("missing room subtype", kind)
	return protocol.Packet{}
}
func (p *testPeer) hostPacket(kind int) protocol.Packet {
	p.t.Helper()
	for i := 0; i < 200; i++ {
		v := p.next(68)
		if len(v.Data) > 0 && int(v.Data[0]) == kind {
			return v
		}
	}
	p.t.Fatal("missing host subtype", kind)
	return protocol.Packet{}
}
func (p *testPeer) waitReady(id, value int) {
	p.t.Helper()
	for i := 0; i < 200; i++ {
		v := p.roomPacket(3)
		r := &protocol.Reader{Data: v.Data}
		r.U8()
		if r.U32() == id && r.U8() == value {
			return
		}
	}
	p.t.Fatal("missing ready state")
}
func TestLoginRoomUDPAndShutdown(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		crypt, dedicated bool
	}{{"plain", false, false}, {"crypt", true, false}, {"dedicated", false, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			dir := t.TempDir()
			if e := assets.Init(dir); e != nil {
				t.Fatal(e)
			}
			cfg, e := config.Load(filepath.Join(dir, "ServerConfig.json"))
			if e != nil {
				t.Fatal(e)
			}
			// The shipped client config allows one registration per IP; this
			// integration test creates multiple local accounts.
			cfg.MaxRegistrationsPerIP = 0
			cfg.Crypt = scenario.crypt
			if scenario.dedicated {
				cfg.Room.HostConnectingMethod = 1
			}
			db, e := store.Open(filepath.Join(dir, "UserDatabase.db3"))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			srv, e := New(dir, cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
			if e != nil {
				t.Fatal(e)
			}
			tcp, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			addr, _ := net.ResolveUDPAddr("udp4", tcp.Addr().String())
			udp, e := net.ListenUDP("udp4", addr)
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- srv.Serve(ctx, tcp, udp) }()
			defer func() {
				cancel()
				select {
				case e := <-done:
					if e != nil {
						t.Error(e)
					}
				case <-time.After(5 * time.Second):
					t.Error("shutdown hung")
				}
			}()
			makePeer := func(name string) *testPeer {
				conn, e := net.Dial("tcp4", tcp.Addr().String())
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { conn.Close() })
				conn.SetDeadline(time.Now().Add(15 * time.Second))
				g := make([]byte, len(protocol.Greeting))
				if _, e = io.ReadFull(conn, g); e != nil || string(g) != protocol.Greeting {
					t.Fatal(string(g), e)
				}
				p := &testPeer{conn, protocol.NewCodec(), t}
				w := &protocol.Writer{}
				w.U8(67)
				w.U16(26)
				w.U32(0)
				w.U32(0)
				p.send(0, w)
				p.next(0)
				if scenario.crypt {
					w = &protocol.Writer{}
					w.String("steam")
					w.U16(0)
					hwid := []byte("0123456789abcdef")
					w.Bytes(hwid)
					w.U32(0)
					w.U32(0)
					w.String("en")
					p.send(3, w)
					p.next(7)
					key, _ := protocol.DeriveKey(hwid)
					p.codec.In, _ = rc4.NewCipher(key[:16])
					p.next(7)
					p.send(12, &protocol.Writer{})
					p.codec.Out, _ = rc4.NewCipher(key[:16])
				}
				p.command("/register " + name + " secret1")
				p.next(67)
				p.command("/login " + name + " secret1")
				p.next(1)
				p.next(6)
				w = &protocol.Writer{}
				w.String(name)
				p.send(2, w)
				p.next(150)
				p.next(69)
				return p
			}
			a, b := makePeer("alice"), makePeer("bobby")
			actor := a
			if scenario.dedicated {
				conn, e := net.Dial("tcp4", tcp.Addr().String())
				if e != nil {
					t.Fatal(e)
				}
				defer conn.Close()
				g := make([]byte, len(protocol.Greeting))
				if _, e = io.ReadFull(conn, g); e != nil {
					t.Fatal(e)
				}
				actor = &testPeer{conn, protocol.NewCodec(), t}
				w := &protocol.Writer{}
				w.U8(0)
				w.U16(27015)
				w.Bytes([]byte{127, 0, 0, 1})
				actor.send(81, w)
				actor.next(91)
			}
			// Create a room with a password, then join with another authenticated user.
			w := &protocol.Writer{}
			w.U8(0)
			w.U32(1 | 8 | 64 | 128 | 256)
			w.U32(0)
			w.U32(0)
			w.U32(0)
			w.String("test room")
			w.String("secret")
			w.U8(0)
			w.U16(1)
			w.U8(4)
			a.send(65, w)
			created := a.roomPacket(0)
			cr := &protocol.Reader{Data: created.Data}
			cr.U8()
			cr.U8()
			if cr.U32() != 1 || cr.U32() != 1 {
				t.Fatal("wrong room or host", created.Data)
			}
			w = &protocol.Writer{}
			w.U8(23)
			w.U8(0)
			w.U16(1)
			w.String("secret")
			b.send(65, w)
			joined := b.roomPacket(0)
			jr := &protocol.Reader{Data: joined.Data}
			jr.Bytes(13)
			if _, e := parseSettings(jr); e != nil {
				t.Fatal(e)
			}
			if jr.U8() != 2 {
				t.Fatal("room member count is not two")
			}
			arrived := a.roomPacket(1)
			ar := &protocol.Reader{Data: arrived.Data}
			ar.U8()
			if ar.U32() != 2 {
				t.Fatal("wrong joining user")
			}
			// Start a peer-hosted match, feed the host inventory and counters,
			// settle once, then verify a fresh match can migrate to a new host.
			b.send(65, &protocol.Writer{Data: []byte{3}})
			b.waitReady(2, 2)
			a.send(65, &protocol.Writer{Data: []byte{4}})
			actor.hostPacket(0)
			if scenario.dedicated {
				a.hostPacket(5)
				b.hostPacket(5)
			} else {
				b.hostPacket(1)
			}
			for _, id := range []int{1, 2} {
				w = &protocol.Writer{}
				w.U8(101)
				w.U32(id)
				actor.send(68, w)
				inventory := actor.hostPacket(101)
				ir := &protocol.Reader{Data: inventory.Data}
				ir.U8()
				if ir.U32() != id || ir.U16() == 0 {
					t.Fatal("missing host inventory")
				}
				ir = &protocol.Reader{Data: inventory.Data}
				ir.U8()
				ir.U32()
				count := ir.U16()
				starterWeapon := false
				for i := 0; i < count; i++ {
					item := ir.U16()
					ir.Bytes(11)
					ir.Bytes(ir.U8() * 3)
					starterWeapon = starterWeapon || item == 12
				}
				if ir.Err != nil || !starterWeapon {
					t.Fatal("starter MP5 excluded from host inventory", ir.Err)
				}
			}
			w = &protocol.Writer{}
			w.U8(7)
			w.U32(1)
			w.U8(0)
			w.U32(2)
			w.U8(0)
			actor.send(68, w)
			actor.send(68, &protocol.Writer{Data: []byte{5}})
			update := a.next(157)
			ur := &protocol.Reader{Data: update.Data}
			if ur.U32() != 1 || ur.U32()&(1<<7|1<<8) != 0 {
				t.Fatal("result UI received already-incremented balance or counters")
			}
			a.hostPacket(3)
			a.roomPacket(6)
			b.hostPacket(3)
			b.roomPacket(6)
			character, e := db.Character(1)
			if e != nil || character.Exp != 460 || character.Points != cfg.DefaultUser.Points+400 || character.Kills != 2 {
				t.Fatal("wrong settlement", character, e)
			}
			a.send(65, &protocol.Writer{Data: []byte{6}})
			a.hostPacket(4)
			update = a.next(157)
			ur = &protocol.Reader{Data: update.Data}
			if ur.U32() != 1 || ur.U32()&(1<<7) == 0 {
				t.Fatal("closing result did not synchronize balance")
			}
			actor.send(68, &protocol.Writer{Data: []byte{5}})
			b.send(65, &protocol.Writer{Data: []byte{3}})
			b.waitReady(2, 2)
			a.send(65, &protocol.Writer{Data: []byte{4}})
			actor.hostPacket(0)
			if scenario.dedicated {
				a.hostPacket(5)
				b.hostPacket(5)
			} else {
				b.hostPacket(1)
			}
			for _, id := range []int{1, 2} {
				w = &protocol.Writer{}
				w.U8(101)
				w.U32(id)
				actor.send(68, w)
				actor.hostPacket(101)
			}
			w = &protocol.Writer{}
			w.U8(1)
			w.U16(3)
			w.Bytes([]byte{1, 2, 3})
			actor.send(68, w)
			// UDP punch only accepts a logged-in user from the TCP peer's IP.
			uc, e := net.DialUDP("udp4", nil, addr)
			if e != nil {
				t.Fatal(e)
			}
			defer uc.Close()
			w = &protocol.Writer{}
			w.U8('W')
			w.U32(1)
			w.U8(0)
			w.U8(0)
			w.Bytes([]byte{128, 255, 255, 254})
			w.U16(27005)
			w.U8(1)
			uc.Write(w.Data)
			uc.SetReadDeadline(time.Now().Add(3 * time.Second))
			buf := make([]byte, 10)
			n, e := uc.Read(buf)
			if e != nil || string(buf[:n]) != "W\x00\x01" {
				t.Fatal(buf[:n], e)
			}
			a.conn.Close()
			b.roomPacket(2)
			host := b.roomPacket(5)
			hr := &protocol.Reader{Data: host.Data}
			hr.U8()
			if hr.U32() != 2 {
				t.Fatal("host did not transfer")
			}
			if !scenario.dedicated {
				restarted := b.hostPacket(2)
				rr := &protocol.Reader{Data: restarted.Data}
				rr.U8()
				if rr.U32() != 2 || rr.U16() != 3 || string(rr.Bytes(3)) != "\x01\x02\x03" {
					t.Fatal("match migration did not retain saved state")
				}
			} else {
				b.send(65, &protocol.Writer{Data: []byte{6}})
				b.hostPacket(4)
			}
			b.conn.Close()
		})
	}
}
func FuzzRoomSettings(f *testing.F) {
	f.Add(make([]byte, 16))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 65536 {
			return
		}
		r := &protocol.Reader{Data: b}
		s, e := parseSettings(r)
		if e == nil {
			w := &protocol.Writer{}
			s.write(w)
			r2 := &protocol.Reader{Data: w.Data}
			if _, e := parseSettings(r2); e != nil {
				t.Fatal(e)
			}
		}
	})
}
