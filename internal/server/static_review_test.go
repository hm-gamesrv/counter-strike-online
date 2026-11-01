package server

import (
	"encoding/binary"
	"math"
	"net"
	"strings"
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func TestInventoryVariableSizeFrames(t *testing.T) {
	items := make([]store.Item, 400)
	for i := range items {
		items[i] = store.Item{Slot: i + 1, ID: 12, Count: 1, Paints: make([]int, 100)}
	}
	packets, err := inventoryPackets(items, 2)
	if err != nil || len(packets) < 2 {
		t.Fatal("variable-size inventory was not split", len(packets), err)
	}
	total := 0
	for _, data := range packets {
		if len(data) > protocol.MaxPayload {
			t.Fatal("oversized inventory frame", len(data))
		}
		total += int(binary.LittleEndian.Uint16(data[4:6]))
	}
	if total != len(items) {
		t.Fatal("inventory entries lost or repeated", total)
	}
	for _, item := range []store.Item{
		{Slot: 1, ID: 12, Count: 1, Paints: make([]int, protocol.MaxPayload/2)},
		{Slot: math.MaxInt, ID: 12, Count: 1},
		{Slot: 65534, ID: 12, Count: 1},
	} {
		if packets, err := inventoryPackets([]store.Item{items[0], item}, 2); err == nil || packets != nil {
			t.Fatal("invalid inventory returned partial packets", err)
		}
	}
}

func TestCommandsRequireVersionHandshake(t *testing.T) {
	s := &Server{}
	c := &client{}
	w := &protocol.Writer{}
	w.U8(1)
	w.String("/login alice secret1")
	if err := s.dispatch(c, protocol.Packet{ID: 67, Data: w.Data}); err == nil {
		t.Fatal("login without version handshake accepted")
	}
}

func TestMatchRewardDoesNotOverflowBeforeCap(t *testing.T) {
	if got := matchBaseReward(math.MaxInt, 1000000); got != 9999 {
		t.Fatal("reward overflowed before cap", got)
	}
	if got := matchBaseReward(10, 0); got != 10 {
		t.Fatal("zero-kill minimum changed", got)
	}
}

func TestWireCapacityBoundaries(t *testing.T) {
	cfg := config.Config{MaxPlayers: maxLobbyPlayers(), InventorySlotMax: (protocol.MaxPayload - 7) / hostItemMaxSize}
	if err := validateWireLimits(cfg); err != nil {
		t.Fatal(err)
	}
	cfg.MaxPlayers++
	if err := validateWireLimits(cfg); err == nil {
		t.Fatal("oversized lobby configuration accepted")
	}
	cfg.MaxPlayers--
	cfg.InventorySlotMax++
	if err := validateWireLimits(cfg); err == nil {
		t.Fatal("oversized host inventory configuration accepted")
	}
	cfg.InventorySlotMax--
	cfg.DefaultUser.DefaultItems = []int{12}
	if err := validateWireLimits(cfg); err == nil {
		t.Fatal("default items excluded from host inventory capacity")
	}
}

func TestRoomSettingsReserveSpaceForEveryMember(t *testing.T) {
	conn, peer := net.Pipe()
	defer conn.Close()
	defer peer.Close()
	c := &client{conn: conn, user: store.User{Name: strings.Repeat("X", store.MaxUsernameLength)}, character: store.Character{Name: strings.Repeat("X", store.MaxCharacterNameLength)}}
	member := &protocol.Writer{}
	roomUser(member, c)
	budget := protocol.MaxPayload - 14 - 6 - maxRoomMembers*len(member.Data)
	st := settings{}
	st.flags[0] = 1
	st.fields[0] = make([]byte, budget-16) // Settings include four uint32 flag groups.
	if err := validateRoomFrame(st); err != nil {
		t.Fatal("exact boundary rejected", err)
	}
	st.fields[0] = append(st.fields[0], 0)
	if err := validateRoomFrame(st); err == nil {
		t.Fatal("settings leave no space for joining members")
	}
}
