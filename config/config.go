// Package config loads, validates, and applies application configuration from a TOML file.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	minStopTimeout     = time.Second
	defaultStopTimeout = 30 * time.Second
)

// Config contains the application configuration.
type Config struct {
	Minecraft MinecraftConfig `toml:"minecraft"`
	Options   Options         `toml:"options"`
}

// MinecraftConfig contains configuration for launching and managing a Minecraft server.
type MinecraftConfig struct {
	Directory   string        `toml:"directory"`
	Command     string        `toml:"command"`
	JarFile     string        `toml:"jar_file"`
	MinMemory   string        `toml:"min_memory"`
	MaxMemory   string        `toml:"max_memory"`
	Gui         bool          `toml:"gui"`
	StopTimeout time.Duration `toml:"stop_timeout"`
}

// Options contains application-level options.
type Options struct {
	LogLevel string `toml:"log_level"`
}

const configFile = "./config/config.toml"

// LoadConfig reads the configuration file, applies default values, and validates the resulting configuration.
//
// It returns an error if the file cannot be decoded or the configuration is invalid.
func LoadConfig() (Config, error) {
	var conf Config

	_, err := toml.DecodeFile(configFile, &conf)
	if err != nil {
		return conf, fmt.Errorf("cannot decode config file %s: %w", configFile, err)
	}

	if conf.Minecraft.StopTimeout == 0 {
		conf.Minecraft.StopTimeout = defaultStopTimeout
	}

	err = conf.validate()
	if err != nil {
		return conf, fmt.Errorf("invalid config file: %w", err)
	}

	return conf, nil
}

// validate checks that required configuration values are present and that the stop timeout meets the minimum allowed duration.
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

	var problems []string
	if len(missing) > 0 {
		problems = append(problems, fmt.Sprintf("missing required config keys: %s", strings.Join(missing, ", ")))
	}
	if c.Minecraft.StopTimeout < minStopTimeout {
		problems = append(problems, fmt.Sprintf("minecraft.stop_timeout must be at least %s", minStopTimeout))
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}

	return nil
}

// InitLogger configures the default slog logger to write JSON-formatted log entries to standard output using the configured minimum log level.
//
// Unrecognized log levels default to info.
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
