package server

import (
	"fmt"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestReview4308HostCannotConsumeSelectedWeapon(t *testing.T) {
	s, room, stat := rewardMatch(t)
	s.users = map[int]*client{1: stat.user}
	if _, err := s.Store.GrantReward(1, store.Grant{Items: []store.Reward{{
		ID:    12,
		Count: 1,
	}}}, 100, 0); err != nil {
		t.Fatal(err)
	}
	w := &protocol.Writer{}
	w.U8(100)
	w.U32(1)
	w.U16(12)
	w.U16(1)
	if err := s.hostRequest(room.host, &protocol.Reader{Data: w.Data}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.Store.DB.QueryRow("SELECT count(*) FROM UserInventory WHERE userID=1 AND itemID=12").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("host subtype 100 consumed a selected weapon; baseline UseItem refuses status=1")
	}
}

func TestReview4308LobbyBudgetIncludesClanFields(t *testing.T) {
	s := &Server{clients: map[*client]bool{}}
	for i := 1; i <= maxLobbyPlayers(); i++ {
		c := &client{
			user: store.User{
				ID:   i,
				Name: fmt.Sprintf("user%011d", i),
			},
			character: store.Character{
				ID:       i,
				Name:     fmt.Sprintf("char%012d", i),
				ClanID:   1,
				ClanName: "ReviewClan",
			},
			inLobby: true,
			queue:   make(chan outbound, 8),
		}
		s.clients[c] = true
	}
	cfg := config.Config{
		MaxPlayers:       maxLobbyPlayers(),
		InventorySlotMax: 1,
	}
	if err := validateWireLimits(cfg); err != nil {
		t.Fatal(err)
	}
	var target *client
	for c := range s.clients {
		target = c
		break
	}
	s.joinLobby(target)
	for len(target.queue) > 0 {
		packet := <-target.queue
		if len(packet.data) > protocol.MaxPayload {
			t.Fatalf("accepted MaxPlayers=%d produced packet=%d payload=%d over %d", cfg.MaxPlayers, packet.id, len(packet.data), protocol.MaxPayload)
		}
	}
}
