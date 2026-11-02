package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/protocol"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func writeTestCertificate(t *testing.T, dir string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certDir := filepath.Join(dir, "Data", "Certs")
	if err = os.MkdirAll(certDir, 0o750); err != nil {
		t.Fatal(err)
	}
	certFile, err := os.Create(filepath.Join(certDir, "server-cert.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if err = pem.Encode(certFile, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatal(err)
	}
	if err = certFile.Close(); err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyFile, err := os.Create(filepath.Join(certDir, "server-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if err = pem.Encode(keyFile, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatal(err)
	}
	if err = keyFile.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSSLListenerHandshake(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	writeTestCertificate(t, dir)
	cfg, err := config.Load(filepath.Join(dir, "ServerConfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MaxRegistrationsPerIP = 0
	cfg.SSL = true
	db, err := store.Open(filepath.Join(dir, "UserDatabase.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv, err := New(dir, cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	tcp, udp := listenTestPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx, tcp, udp) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("shutdown hung")
		}
	}()

	dialer := &tls.Dialer{Config: &tls.Config{
		MinVersion:         tls.VersionTLS13,
		MaxVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true,
	}}
	raw, err := dialer.DialContext(ctx, "tcp4", tcp.Addr().String())
	if err != nil {
		t.Fatal("TLS dial:", err)
	}
	defer raw.Close()
	raw.SetDeadline(time.Now().Add(10 * time.Second))
	greeting := make([]byte, len(protocol.Greeting))
	if _, err = io.ReadFull(raw, greeting); err != nil || string(greeting) != protocol.Greeting {
		t.Fatalf("greeting %q, %v", greeting, err)
	}
	p := &testPeer{conn: raw, codec: protocol.NewCodec(), t: t}
	w := &protocol.Writer{}
	w.U8(67)
	w.U16(26)
	w.U32(0)
	w.U32(0)
	p.send(0, w)
	p.next(0)
}

func TestSSLRequiresCertificate(t *testing.T) {
	dir := t.TempDir()
	if err := assets.Init(dir); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(dir, "ServerConfig.json"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.SSL = true
	db, err := store.Open(filepath.Join(dir, "UserDatabase.db3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = New(dir, cfg, db, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil {
		t.Fatal("SSL without certificates should fail")
	}
}
