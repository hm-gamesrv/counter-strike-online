package server

import (
	"encoding/binary"

	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

// sendVoxelURLs sends packet 103/20. The reference calls it unconditionally at
// the end of the login chain (src/manager/usermanager.cpp:637), so an unset
// configuration still produces the frame with two empty strings.
func (s *Server) sendVoxelURLs(c *client) {
	w := &protocol.Writer{}
	w.U8(20)
	w.String(s.Config.Voxel.VoxelVxlURL)
	w.String(s.Config.Voxel.VoxelVmgURL)
	c.send(103, w)
}

// voxelBlock decodes the Voxel settings payload: the slot flag followed by the
// indicated string and integer fields. BuildRoomInfo (src/manager/packetmanager.cpp:1965)
// writes the flag and, for a Studio room, the title and image id only.
func (s *settings) voxelBlock() (flag uint32, title, image string) {
	b := s.fields[50]
	if len(b) < 4 {
		return 0, "", ""
	}
	flag = binary.LittleEndian.Uint32(b)
	r := &protocol.Reader{Data: b[4:]}
	for bit := 0; bit < 25 && r.Err == nil; bit++ {
		if flag&(1<<uint(bit)) == 0 {
			continue
		}
		switch bit {
		case 0, 1, 3, 6, 7, 8, 9, 10, 22:
			text := r.CString()
			switch bit {
			case 3:
				title = text
			case 8:
				image = text
			}
		case 2, 4, 5, 19, 21, 23, 24:
			r.U8()
		case 11, 12, 13, 15, 16, 17:
			r.U32()
		case 14:
			n := r.U32()
			for i := 0; i < n && r.Err == nil; i++ {
				r.U32()
				r.CString()
			}
		}
	}
	return flag, title, image
}

func (s *Server) voxelRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	if r.Err != nil {
		return r.Err
	}
	w := &protocol.Writer{}
	w.U8(kind)
	switch kind {
	case 4, 8:
		w.U8(0)
	case 9:
		w.U8(0)
		w.U8(0)
	case 10:
		w.U8(0)
	case 38:
		w.U8(0)
		w.U16(0)
	case 46:
		w.U16(0)
	case 47:
		w.String("")
	case 58:
		w.U8(6)
	default:
		if s.Log != nil {
			s.Log.Debug("unsupported voxel request", "kind", kind, "user", c.user.ID)
		}
		return nil
	}
	c.send(103, w)
	return nil
}
