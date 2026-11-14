package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/emiHuy/mc-server-manager/api"
	"github.com/emiHuy/mc-server-manager/backup"
	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "token" {
		b := make([]byte, 32)
		rand.Read(b)
		fmt.Println(hex.EncodeToString(b))
		return
	}

	conf, err := config.LoadConfig()
	if err != nil {
		slog.Error("config load failed", "error", err)
		return
	}

	conf.InitLogger()
	slog.Info("application starting")

	if conf.API.Host == config.TailscaleHost {
		host, err := waitForTailscale(context.Background(), net.InterfaceAddrs, 500*time.Millisecond, 30*time.Second)
		if err != nil {
			slog.Error("failed to find tailscale host", "error", err)
			return
		}
		conf.API.Host = host.String()
		slog.Info("resolved tailscale host", "host", host.String())
	}

	instance := minecraft.New(conf.Minecraft)
	if err := instance.CheckStale(); err != nil {
		slog.Error("startup check failed", "error", err)
		os.Exit(1)
	}

	restarter := minecraft.NewRestarter(
		instance,
		conf.AutoRestart.Enabled,
		conf.AutoRestart.MaxRestarts,
		conf.AutoRestart.InitialBackoff,
	)

	backups := backup.New(instance, conf.Minecraft.Directory, conf.Backup)

	server := api.New(conf.API, instance, restarter, backups)
	slog.Info("starting api server")

	apiErr := make(chan error, 1)
	go func() { apiErr <- server.Run() }()

	if conf.Minecraft.Autostart {
		err = instance.Start()
		if err != nil {
			slog.Error("minecraft server start failed", "error", err)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sig:
		slog.Info("interrupt received")
	case err := <-apiErr:
		slog.Error("api server failed", "error", err)
	}

	shutdown(instance, restarter, server, sig)
}
