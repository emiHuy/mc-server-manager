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

	err = conf.validate()
	if err != nil {
		return conf, fmt.Errorf("invalid config file: %w", err)
	}

	return conf, nil
}

func (c *Config) validate() error {
	type requiredField struct {
		key   string
		value string
	}

	required := []requiredField{
		{key: "minecraft.directory", value: c.Minecraft.Directory},
		{key: "minecraft.command", value: c.Minecraft.Command},
		{key: "minecraft.jar_file", value: c.Minecraft.JarFile},
		{key: "minecraft.min_memory", value: c.Minecraft.MinMemory},
		{key: "minecraft.max_memory", value: c.Minecraft.MaxMemory},
	}
	var missing []string

	for _, field := range required {
		if field.value == "" {
			missing = append(missing, field.key)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing config value: %s", strings.Join(missing, ", "))
	}

	return nil
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
