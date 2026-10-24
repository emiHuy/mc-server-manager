package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/emiHuy/mc-server-manager/api"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

const (
	killWait           = 5 * time.Second
	apiShutdownTimeout = 5 * time.Second
)

type stopper interface {
	Stop() error
	Kill() error
}

func stopMinecraft(instance stopper, sig <-chan os.Signal) {
	stopResult := make(chan error, 1)
	go func() { stopResult <- instance.Stop() }()

	select {
	case err := <-stopResult:
		logStopResult(err, false)
		return
	case <-sig:
		slog.Warn("second interrupt received, killing minecraft server")
		err := instance.Kill()
		if err != nil {
			slog.Error("minecraft server kill failed", "error", err)
		}
	}

	select {
	case err := <-stopResult:
		logStopResult(err, true)
	case <-time.After(killWait):
		slog.Error("minecraft server exit not confirmed after kill", "timeout", killWait.String())
	}
}

func shutdown(instance *minecraft.Instance, server *api.Server, sig <-chan os.Signal) {
	select {
	case <-instance.Done(): // nothing is running
	default:
		stopMinecraft(instance, sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiShutdownTimeout)
	defer cancel()

	err := server.Shutdown(ctx)
	if err != nil {
		slog.Error("api server shutdown failed", "error", err)
		return
	}

	slog.Info("api server closed")
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
