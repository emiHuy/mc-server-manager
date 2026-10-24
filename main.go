package main

import (
	"log/slog"
	"os"
	"os/signal"

	"github.com/emiHuy/mc-server-manager/api"
	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

func main() {
	conf, err := config.LoadConfig()
	if err != nil {
		slog.Error("config load failed", "error", err)
		return
	}

	conf.InitLogger()
	slog.Info("application starting")

	instance := minecraft.New(conf.Minecraft)
	server := api.New(instance, conf.API)
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
	signal.Notify(sig, os.Interrupt)

	select {
	case <-sig:
		slog.Info("interrupt received")
	case err := <-apiErr:
		slog.Error("api server failed", "error", err)
	}

	shutdown(instance, server, sig)
}
