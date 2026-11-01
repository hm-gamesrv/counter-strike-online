package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hm-gamesrv/cso-server-go/assets"
	"github.com/hm-gamesrv/cso-server-go/internal/config"
	"github.com/hm-gamesrv/cso-server-go/internal/server"
	"github.com/hm-gamesrv/cso-server-go/internal/store"
)

func run() error {
	dir := flag.String("data", ".data", "data directory")
	addr := flag.String("listen", "", "TCP/UDP bind address (default 0.0.0.0:config.Port)")
	initOnly := flag.Bool("init", false, "initialize missing default files and exit")
	debug := flag.Bool("debug", false, "enable debug logging")
	flag.Parse()
	if e := assets.Init(*dir); e != nil {
		return e
	}
	if *initOnly {
		return nil
	}
	cfg, e := config.Load(filepath.Join(*dir, "ServerConfig.json"))
	if e != nil {
		return e
	}
	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	db, e := store.Open(filepath.Join(*dir, "UserDatabase.db3"))
	if e != nil {
		return e
	}
	defer db.Close()
	srv, e := server.New(*dir, cfg, db, log)
	if e != nil {
		return e
	}
	bind := *addr
	if bind == "" {
		bind = net.JoinHostPort("0.0.0.0", cfg.Port)
	}
	tcp, e := net.Listen("tcp4", bind)
	if e != nil {
		return e
	}
	defer tcp.Close()
	udpAddr, e := net.ResolveUDPAddr("udp4", tcp.Addr().String())
	if e != nil {
		return e
	}
	udp, e := net.ListenUDP("udp4", udpAddr)
	if e != nil {
		return e
	}
	defer udp.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	log.Info("CSNZ Go server listening", "tcp", tcp.Addr(), "udp", udp.LocalAddr(), "data", *dir, "status", "partial migration")
	return srv.Serve(ctx, tcp, udp)
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
