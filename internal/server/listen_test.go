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
	for attempt := 0; attempt < 30; attempt++ {
		tcp, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr, _ := net.ResolveUDPAddr("udp4", tcp.Addr().String())
		udp, err := net.ListenUDP("udp4", addr)
		if err == nil {
			return tcp, udp
		}
		tcp.Close()
	}
	t.Fatal("could not bind a TCP/UDP test pair")
	return nil, nil
}
