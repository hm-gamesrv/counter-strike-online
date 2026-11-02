package server

import (
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

// ghostRoomPassword keeps the /addgroom debug room out of reach. Upstream uses a
// 60-byte literal there, which exceeds the 32-byte password limit this server
// enforces on the create path, so the fixed value is short instead.
const ghostRoomPassword = "addgroom"

// ghostRoom creates the room /addgroom builds: hard-coded name, password and
// settings (mode 15, map 128, 10 players). The settings go back through the
// ordinary create path so validation, defaults and broadcasts stay identical.
func (s *Server) ghostRoom(c *client) error {
	if c.room != 0 || !c.inLobby {
		c.chatNotice("/addgroom requires the lobby")
		return nil
	}
	var st settings
	setText := func(index int, value string) {
		w := &protocol.Writer{}
		w.String(value)
		st.flags[index/32] |= 1 << (index % 32)
		st.fields[index] = w.Data
	}
	setText(0, "GHOST ROOM NAME")
	setText(3, ghostRoomPassword)
	st.setNumber(6, 15, 1)
	st.setNumber(7, 128, 2)
	st.setNumber(8, 10, 1)
	w := &protocol.Writer{}
	w.U8(0)
	st.write(w)
	return s.roomRequest(c, &protocol.Reader{Data: w.Data})
}
