package server

import (
	"fmt"
	"strings"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

const maxRoomMembers = 32
const networkInfoSize = 20
const hostItemMaxSize = 20 // Fixed 14 bytes plus two three-byte parts.

func maxUserInfoSize() int {
	w := &protocol.Writer{}
	userInfo(w, store.Character{Name: strings.Repeat("X", store.MaxCharacterNameLength)})
	return len(w.Data)
}

func maxLobbyPlayers() int {
	// Full lobby list: kind + uint16 count, then ID, username and user info.
	return (protocol.MaxPayload - 3) / (4 + store.MaxUsernameLength + 1 + maxUserInfoSize())
}

func validateWireLimits(c config.Config) error {
	if c.MaxPlayers < 1 || c.MaxPlayers > maxLobbyPlayers() {
		return fmt.Errorf("MaxPlayers must be 1..%d to fit the current client's lobby frame", maxLobbyPlayers())
	}
	// Host inventory has no established continuation format. Every active item
	// must fit, including defaults and both optional parts on every owned item.
	capacity := (protocol.MaxPayload-7)/hostItemMaxSize - len(c.DefaultUser.DefaultItems)
	if c.InventorySlotMax < 1 || c.InventorySlotMax > capacity {
		return fmt.Errorf("InventorySlotMax must be 1..%d to fit the current client's host inventory frame", capacity)
	}
	return nil
}

func validateRoomFrame(st settings) error {
	w := &protocol.Writer{}
	st.write(w)
	memberSize := 8 + store.MaxUsernameLength + 1 + networkInfoSize + maxUserInfoSize()
	// Joined header is 14 bytes. Reserve 6 more bytes for status and map fields
	// that the server may insert later, and room for all 32 members.
	if len(w.Data) > protocol.MaxPayload-14-6-maxRoomMembers*memberSize {
		return fmt.Errorf("room settings exceed joined-room frame limit")
	}
	return nil
}
