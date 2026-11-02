package server

import (
	"path/filepath"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func bingoServer(t *testing.T) (*Server, *client, int) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "bingo.db3"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err = db.Register("bingo", "secret1", "127.0.0.1", 0); err != nil {
		t.Fatal(err)
	}
	if err = db.CreateCharacter(1, "Bingo", config.DefaultUser{Level: 1}); err != nil {
		t.Fatal(err)
	}
	prizes := make([]bingoPrizeConfig, 12)
	for i := range prizes {
		prizes[i] = bingoPrizeConfig{
			ItemID:   567,
			Count:    1,
			Duration: 30,
		}
	}
	s := &Server{
		Store: db,
		Config: config.Config{
			InventorySlotMax: 3000,
		},
		items: map[int]itemDefinition{
			567: {
				Category: 1,
			},
		},
		bingo: bingoDefinition{
			Active: true,
			Prizes: prizes,
		},
		clients: map[*client]bool{},
		users:   map[int]*client{},
	}
	c := &client{
		user: store.User{
			ID:   1,
			Name: "bingo",
		},
		character: store.Character{
			ID:   1,
			Name: "Bingo",
		},
		queue: make(chan outbound, 256),
	}
	s.clients[c] = true
	s.users[1] = c
	return s, c, 1
}

func TestBingoUpdateAndOpenLine(t *testing.T) {
	s, c, id := bingoServer(t)
	if err := s.dispatch(c, protocol.Packet{ID: 89, Data: []byte{0, 0}}); err != nil {
		t.Fatal(err)
	}
	message := <-c.queue
	reader := &protocol.Reader{Data: message.data}
	if message.id != 89 || reader.U8() != 0 || reader.U8() != 0 || reader.U8() != 25 {
		t.Fatalf("bingo update %v", message.data)
	}
	for i := 0; i < 25; i++ {
		reader.U8()
		reader.U8()
	}
	if reader.U8() != 12 {
		t.Fatalf("prize count %v", message.data)
	}
	for i := 0; i < 12; i++ {
		reader.U16()
		reader.U8()
		reader.U8()
		reader.U8()
	}
	slots, err := s.Store.BingoSlots(id)
	if err != nil {
		t.Fatal(err)
	}
	for i := range slots {
		slots[i].Opened = 1
	}
	slots[0].Opened = 0
	if err = s.Store.SetBingoSlots(id, slots); err != nil {
		t.Fatal(err)
	}
	if err = s.bingoOpen(c); err != nil {
		t.Fatal(err)
	}
	prizes, err := s.Store.BingoPrizes(id)
	if err != nil {
		t.Fatal(err)
	}
	opened := 0
	for _, prize := range prizes {
		if prize.Opened != 0 {
			opened++
		}
	}
	if opened != 12 {
		t.Fatalf("opened prizes %d", opened)
	}
	items, err := s.Store.Inventory(id)
	if err != nil {
		t.Fatal(err)
	}
	weapons := 0
	for _, item := range items {
		if item.ID == 567 {
			weapons++
		}
	}
	if weapons < 12 {
		t.Fatalf("bingo rewards %d", weapons)
	}
}
