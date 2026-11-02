package server

import (
	"net"
	"testing"
)

// listenTestPair binds a TCP listener and a UDP socket on the same ephemeral
// port. Windows sometimes rejects a UDP bind for a port picked by the TCP
// listener (reserved or excluded ranges), so retry with a fresh port.
func listenTestPair(t *testing.T) (net.Listener, *net.UDPConn) {
	t.Helper()
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 30; attempt++ {
		udp, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
		if err == nil {
			return tcp, udp
		}
	}
	tcp.Close()
	t.Fatal("could not bind a UDP test socket")
	return nil, nil
}
