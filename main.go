package main

import (
	"log"
	"log/slog"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

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
		slog.Error("failed to start minecraft server", "error", err)
	}

	time.Sleep(15 * time.Second)
	if err := instance.Stop(); err != nil {
		log.Fatal(err)
	}
	time.Sleep(15 * time.Second)
}
