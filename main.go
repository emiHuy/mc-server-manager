package main

import (
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

const killWait = 5 * time.Second

func main() {
	conf, err := config.LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return
	}

	conf.InitLogger()

	slog.Info("application starting")
	slog.Debug("configuration loaded")

	instance := minecraft.New(conf.Minecraft)
	err = instance.Start()
	if err != nil {
		slog.Error("minecraft server start failed", "error", err)
		return
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	done := instance.Done()

	select {
	case <-done:
		slog.Info("minecraft server exited on its own")
		return
	case <-sig:
		slog.Info("interrupt received")
	}

	stopResult := make(chan error, 1)
	go func() {
		stopResult <- instance.Stop()
	}()

	select {
	case err := <-stopResult:
		logStopResult(err, false)
		return
	case <-sig:
		slog.Warn("second interrupt received, killing minecraft server")
		err = instance.Kill()
		if err != nil {
			slog.Error("minecraft server kill request failed", "error", err)
		}
	}

	select {
	case err := <-stopResult:
		logStopResult(err, true)
	case <-time.After(killWait):
		slog.Error("minecraft server did not confirm exit after kill", "timeout", killWait)
	}
}

func logStopResult(err error, forced bool) {
	switch {
	case err != nil:
		slog.Error("minecraft server stop failed", "error", err)
	case forced:
		slog.Info("minecraft server stopped after kill")
	default:
		slog.Info("minecraft server stopped gracefully")
	}
}
