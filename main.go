package main

import (
	"log/slog"

	"github.com/emiHuy/mc-server-manager/config"
)

func main() {
	conf, err := config.LoadConfig()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		return
	}

	conf.InitLogger()

	slog.Info("application starting")
	slog.Debug("configuration loaded", "config", conf)
}
