package server

import (
	"testing"

	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
)

func TestVoxelURLsAndPlaceholders(t *testing.T) {
	s := &Server{
		Config: config.Config{
			Voxel: config.Voxel{
				VoxelVxlURL: "http://example/%s.vxl",
				VoxelVmgURL: "http://example/%s.vmg",
			},
		},
	}
	c := &client{
		queue: make(chan outbound, 8),
	}
	s.sendVoxelURLs(c)
	message := <-c.queue
	reader := &protocol.Reader{Data: message.data}
	if message.id != 103 || reader.U8() != 20 || reader.CString() != "http://example/%s.vxl" || reader.CString() != "http://example/%s.vmg" {
		t.Fatalf("voxel URLs %v", message.data)
	}
	for _, kind := range []int{4, 8, 9, 10, 38, 46, 47, 58} {
		if err := s.voxelRequest(c, &protocol.Reader{Data: []byte{byte(kind)}}); err != nil {
			t.Fatal(err)
		}
		message = <-c.queue
		reader = &protocol.Reader{Data: message.data}
		if message.id != 103 || reader.U8() != kind {
			t.Fatalf("voxel %d %v", kind, message.data)
		}
	}
}
