package server

import (
	"errors"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"net"
	"slices"
)

type dedicatedServer struct {
	client                *client
	ip                    net.IP
	port, room, memoryMiB int
}

func (s *Server) dedicatedRequest(c *client, r *protocol.Reader) error {
	kind := r.U8()
	switch kind {
	case 0:
		if c.user.ID != 0 || c.dedicated != nil {
			return errors.New("duplicate or authenticated dedicated registration")
		}
		if !slices.Contains(s.Config.DedicatedServerWhitelist, c.ip()) {
			return errors.New("dedicated server IP not whitelisted")
		}
		port := r.U16()
		ip := append(net.IP{}, r.Bytes(4)...)
		if r.Err != nil {
			return r.Err
		}
		if port == 0 {
			return errors.New("invalid dedicated port")
		}
		if ip.Equal(net.IPv4zero) {
			ip = net.ParseIP(c.ip()).To4()
		}
		if ip == nil {
			return errors.New("IPv4 dedicated address required")
		}
		c.dedicated = &dedicatedServer{client: c, ip: ip, port: port}
		s.dedicated = append(s.dedicated, c.dedicated)
		if s.Config.Crypt {
			if e := s.beginCrypt(c); e != nil {
				return e
			}
		}
		for _, data := range s.metadata {
			c.send(91, &protocol.Writer{Data: data})
		}
		s.Log.Info("dedicated registered", "ip", ip, "port", port)
	case 1:
		memory := r.U16()
		if r.Err != nil {
			return r.Err
		}
		if c.dedicated != nil {
			c.dedicated.memoryMiB = memory
		}
	case 2:
		r.U32()
	case 3:
		r.CString()
	}
	return r.Err
}
func (s *Server) removeDedicated(c *client) {
	d := c.dedicated
	if d == nil {
		return
	}
	if room := s.rooms[d.room]; room != nil {
		room.dedicated = nil
		if e := s.endGame(room, true); e != nil {
			s.Log.Error("dedicated disconnect settlement failed", "room", room.id, "error", e)
		}
	}
	s.dedicated = slices.DeleteFunc(s.dedicated, func(peer *dedicatedServer) bool { return peer == d })
	c.dedicated = nil
	c.room = 0
}
func (s *Server) availableDedicated() *dedicatedServer {
	for _, peer := range s.dedicated {
		if peer.room == 0 {
			return peer
		}
	}
	return nil
}
func releaseDedicated(r *room) {
	if d := r.dedicated; d != nil {
		d.client.send(68, &protocol.Writer{Data: []byte{3}})
		d.room = 0
		d.client.room = 0
		r.dedicated = nil
	}
}
