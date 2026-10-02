package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Minecraft MinecraftConfig `toml:"minecraft"`
	Options   Options         `toml:"options"`
}

type MinecraftConfig struct {
	Directory string `toml:"directory"`
	Command   string `toml:"command"`
	JarFile   string `toml:"jar_file"`
	MinMemory string `toml:"min_memory"`
	MaxMemory string `toml:"max_memory"`
	Gui       bool   `toml:"gui"`
}

type Options struct {
	LogLevel string `toml:"log_level"`
}

const configFile = "./config/config.toml"

func LoadConfig() (Config, error) {
	var conf Config

	_, err := toml.DecodeFile(configFile, &conf)
	if err != nil {
		return conf, fmt.Errorf("failed to decode config file %s: %w", configFile, err)
	}

	return conf, nil
}

func (c *Config) InitLogger() {
	var level slog.Level

	switch strings.ToLower(c.Options.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	slog.SetDefault(slog.New(handler))
}
