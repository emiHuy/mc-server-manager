package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/emiHuy/mc-server-manager/api"
	"github.com/emiHuy/mc-server-manager/backup"
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
	go func() {
		stopResult <- instance.Stop()
	}()

	select {
	case err := <-stopResult:
		logStopResult(err, false)
		return
	case <-sig:
		slog.Warn("main: second signal, killing minecraft server")
		err := instance.Kill()
		if err != nil {
			slog.Error("main: failed to kill minecraft server", "error", err)
		}
	}

	select {
	case err := <-stopResult:
		logStopResult(err, true)
	case <-time.After(killWait):
		slog.Error("main: minecraft exit not confirmed after kill", "timeout", killWait.String())
	}
}

func shutdown(instance *minecraft.Instance, restarter *minecraft.Restarter, scheduler *backup.Scheduler, server *api.Server, sig <-chan os.Signal) {
	restarter.Disable()
	scheduler.Stop()

	select {
	case <-instance.Done():
	default:
		stopMinecraft(instance, sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), apiShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("main: failed to shutdown api server", "error", err)
		return
	}

	slog.Info("main: api server closed")
}

func logStopResult(err error, forced bool) {
	switch {
	case err != nil:
		slog.Error("main: failed to stop minecraft server", "error", err)
	case forced:
		slog.Info("main: minecraft server stopped after kill")
	default:
		slog.Info("main: minecraft server stopped gracefully")
	}
}
