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

const (
	pollInterval = 500 * time.Millisecond
	waitTimeout  = 30 * time.Second
)

func main() {
	// CLI command to generate random 256-bit hex token and exit.
	if len(os.Args) > 1 && os.Args[1] == "token" {
		b := make([]byte, 32)
		rand.Read(b)
		fmt.Println(hex.EncodeToString(b))
		return
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		slog.Error("main: failed to load config", "error", err)
		return
	}

	cfg.InitLogger()

	// Wait for a Tailscale IP before binding the API if configured to use it.
	if cfg.API.Host == config.TailscaleHost {
		host, err := waitForTailscale(context.Background(), net.InterfaceAddrs, pollInterval, waitTimeout)
		if err != nil {
			slog.Error("main: failed to resolve tailscale host", "error", err)
			return
		}
		cfg.API.Host = host.String()
		slog.Info("main: resolved tailscale host", "host", host.String())
	}

	instance := minecraft.New(cfg.Minecraft)
	if err := instance.CheckStale(); err != nil {
		slog.Error("main: startup check failed", "error", err)
		os.Exit(1)
	}

	restarter := minecraft.NewRestarter(
		instance,
		cfg.AutoRestart.Enabled,
		cfg.AutoRestart.MaxRestarts,
		cfg.AutoRestart.InitialBackoff,
	)

	backups := backup.New(instance, cfg.Minecraft.Directory, cfg.Backup)

	var scheduler *backup.Scheduler
	if cfg.Backup.Interval > 0 {
		scheduler = backup.NewScheduler(backups, cfg.Backup.Interval)
		scheduler.Start()
		slog.Info("main: scheduled backups enabled", "interval", cfg.Backup.Interval.String(), "keep", cfg.Backup.Keep)
	}

	server := api.New(cfg.API, instance, restarter, backups, api.WithNextBackup(scheduler))
	slog.Info("main: starting api server", "host", cfg.API.Host)

	// Run API asynchronously so main can wait on signals or fatal server errors.
	apiErr := make(chan error, 1)
	go func() {
		apiErr <- server.Run()
	}()

	if cfg.Minecraft.Autostart {
		if err = instance.Start(); err != nil {
			slog.Error("main: failed to start minecraft", "error", err)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	// Block until shutdown signal or API failure, then tear down cleanly.
	select {
	case <-sig:
		slog.Info("main: signal received")
	case err := <-apiErr:
		slog.Error("main: api server failed", "error", err)
	}

	shutdown(instance, restarter, scheduler, server, sig)
}
