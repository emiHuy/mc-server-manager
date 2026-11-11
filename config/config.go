// Package config loads, validates, and applies application configuration from a TOML file.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	minStopTimeout        = time.Second
	defaultStopTimeout    = 30 * time.Second
	minStartupTimeout     = 5 * time.Second
	defaultStartupTimeout = 2 * time.Minute
	defaultHost           = "127.0.0.1"
	defaultPort           = 8080
	minTokenLength        = 16
	placeholderToken      = "your_generated_token_here"
	defaultMaxRestarts    = 5
	defaultInitialBackoff = 5 * time.Second
	minInitialBackoff     = 1 * time.Second
	TailscaleHost         = "tailscale"
)

// Config contains the application configuration.
type Config struct {
	API         APIConfig         `toml:"api"`
	Minecraft   MinecraftConfig   `toml:"minecraft"`
	AutoRestart AutoRestartConfig `toml:"auto_restart"`
	Options     Options           `toml:"options"`
}

type APIConfig struct {
	Host  string `toml:"host"`
	Port  int    `toml:"port"`
	Token string `toml:"token"`
}

// MinecraftConfig contains configuration for launching and managing a Minecraft server.
type MinecraftConfig struct {
	Directory      string        `toml:"directory"`
	Command        string        `toml:"command"`
	JarFile        string        `toml:"jar_file"`
	MinMemory      string        `toml:"min_memory"`
	MaxMemory      string        `toml:"max_memory"`
	Gui            bool          `toml:"gui"`
	StopTimeout    time.Duration `toml:"stop_timeout"`
	StartupTimeout time.Duration `toml:"startup_timeout"`
	Autostart      bool          `toml:"autostart"`
}

type AutoRestartConfig struct {
	Enabled        bool          `toml:"enabled"`
	MaxRestarts    int           `toml:"max_restarts"`
	InitialBackoff time.Duration `toml:"initial_backoff"`
}

// Options contains application-level options.
type Options struct {
	LogLevel string `toml:"log_level"`
}

const configFile = "./config/config.toml"

// LoadConfig reads and validates the config file at configFile.
func LoadConfig() (Config, error) {
	return loadConfig(configFile)
}

// loadConfig decodes the file at path, applies defaults, then validates.
func loadConfig(path string) (Config, error) {
	var conf Config

	_, err := toml.DecodeFile(path, &conf)
	if err != nil {
		return conf, fmt.Errorf("cannot decode config file %s: %w", path, err)
	}

	if conf.API.Host == "" {
		conf.API.Host = defaultHost
	}
	if conf.API.Port == 0 {
		conf.API.Port = defaultPort
	}
	if conf.Minecraft.StopTimeout == 0 {
		conf.Minecraft.StopTimeout = defaultStopTimeout
	}
	if conf.Minecraft.StartupTimeout == 0 {
		conf.Minecraft.StartupTimeout = defaultStartupTimeout
	}

	if conf.AutoRestart.MaxRestarts == 0 {
		conf.AutoRestart.MaxRestarts = defaultMaxRestarts
	}
	if conf.AutoRestart.InitialBackoff == 0 {
		conf.AutoRestart.InitialBackoff = defaultInitialBackoff
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
	if c.Minecraft.StartupTimeout < minStartupTimeout {
		problems = append(problems, fmt.Sprintf("minecraft.startup_timeout must be at least %s", minStartupTimeout))
	}
	if c.API.Port < 1 || c.API.Port > 65535 {
		problems = append(problems, "api.port must be between 1 and 65535")
	}

	if c.API.Token == "" && !isLoopbackHost(c.API.Host) {
		problems = append(problems, fmt.Sprintf(
			"api.token is required when api.host (%q) is not a loopback address; run `mc-server-manager token` to generate one",
			c.API.Host))
	}

	if c.API.Token != "" {
		switch {
		case c.API.Token == placeholderToken:
			problems = append(problems, "api.token is still the example placeholder; run `mc-server-manager token` to generate one")
		case len(c.API.Token) < minTokenLength:
			problems = append(problems, fmt.Sprintf("api.token must be at least %d characters", minTokenLength))
		}
	}

	if c.AutoRestart.MaxRestarts < 1 {
		problems = append(problems, "auto_restart.max_restarts must be at least 1")
	}
	if c.AutoRestart.InitialBackoff < minInitialBackoff {
		problems = append(problems, "auto_restart.initial_backoff must be at least 1 second")
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

// isLoopbackHost reports whether host only accepts connections from this machine.
// An empty host counts as loopback because it defaults to 127.0.0.1.
func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
