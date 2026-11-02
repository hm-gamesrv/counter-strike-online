package server

import (
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func (s *Server) sendVoxelURLs(c *client) {
	if s.Config.Voxel.VoxelVxlURL == "" && s.Config.Voxel.VoxelVmgURL == "" {
		return
	}
	w := &protocol.Writer{}
	w.U8(20)
	w.String(s.Config.Voxel.VoxelVxlURL)
	w.String(s.Config.Voxel.VoxelVmgURL)
	c.send(103, w)
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
