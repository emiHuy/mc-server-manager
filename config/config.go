// Package config loads, validates, and applies application configuration from a TOML file.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	defaultHost      = "127.0.0.1"
	defaultPort      = 8080
	placeholderToken = "your_generated_token_here"
	minTokenLength   = 16

	defaultStopTimeout    = 30 * time.Second
	minStopTimeout        = 1 * time.Second
	defaultStartupTimeout = 2 * time.Minute
	minStartupTimeout     = 5 * time.Second

	defaultMaxRestarts    = 5
	defaultInitialBackoff = 5 * time.Second
	minInitialBackoff     = 1 * time.Second

	defaultBackupDirectory    = "backups"
	defaultBackupWorld        = "world"
	defaultBackupFlushTimeout = 30 * time.Second
	minBackupFlushTimeout     = 5 * time.Second
	minBackupInterval         = 5 * time.Minute

	defaultLogLevel = "info"

	TailscaleHost = "tailscale"

	configFile = "./config/config.toml"
)

// Config contains the entire application configuration.
type Config struct {
	API         APIConfig         `toml:"api"`
	Minecraft   MinecraftConfig   `toml:"minecraft"`
	AutoRestart AutoRestartConfig `toml:"auto_restart"`
	Backup      BackupConfig      `toml:"backup"`
	Options     Options           `toml:"options"`
}

// APIConfig contains the configuration for the API server.
type APIConfig struct {
	Host  string `toml:"host"`
	Port  int    `toml:"port"`
	Token string `toml:"token"`
}

// MinecraftConfig contains the configuration for launching and managing a Minecraft server.
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

// AutoRestartConfig contains the configuration for minecraft server auto restarts after a crash.
type AutoRestartConfig struct {
	Enabled        bool          `toml:"enabled"`
	MaxRestarts    int           `toml:"max_restarts"`
	InitialBackoff time.Duration `toml:"initial_backoff"`
}

// BackupConfig contains the configuration for managing minecraft world backups.
type BackupConfig struct {
	Directory    string        `toml:"directory"`
	Keep         int           `toml:"keep"`
	World        string        `toml:"world"`
	FlushTimeout time.Duration `toml:"flush_timeout"`
	Interval     time.Duration `toml:"interval"`
}

// Options contains application-level options.
type Options struct {
	LogLevel string `toml:"log_level"`
}

// LoadConfig reads and validates the config file at configFile.
func LoadConfig() (Config, error) {
	return loadConfig(configFile)
}

// loadConfig decodes the file at path, applies defaults, then validates.
func loadConfig(path string) (Config, error) {
	var cfg Config

	_, err := toml.DecodeFile(path, &cfg)
	if err != nil {
		return cfg, fmt.Errorf("cannot decode config file %s: %w", path, err)
	}

	cfg.applyDefaults()

	if err = cfg.validate(); err != nil {
		return cfg, fmt.Errorf("invalid config file: %w", err)
	}

	return cfg, nil
}

// InitLogger configures the default slog logger to write JSON-formatted log entries to standard output using the configured minimum log level.
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

func (c *Config) applyDefaults() {
	setDefault(&c.API.Host, defaultHost)
	setDefault(&c.API.Port, defaultPort)

	setDefault(&c.Minecraft.StopTimeout, defaultStopTimeout)
	setDefault(&c.Minecraft.StartupTimeout, defaultStartupTimeout)

	setDefault(&c.AutoRestart.MaxRestarts, defaultMaxRestarts)
	setDefault(&c.AutoRestart.InitialBackoff, defaultInitialBackoff)

	setDefault(&c.Backup.Directory, defaultBackupDirectory)
	setDefault(&c.Backup.World, defaultBackupWorld)
	setDefault(&c.Backup.FlushTimeout, defaultBackupFlushTimeout)

	setDefault(&c.Options.LogLevel, defaultLogLevel)
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

	if c.AutoRestart.MaxRestarts < 0 {
		problems = append(problems, "auto_restart.max_restarts must be at least 0")
	}
	if c.AutoRestart.InitialBackoff < minInitialBackoff {
		problems = append(problems, fmt.Sprintf("auto_restart.initial_backoff must be at least %s second", minInitialBackoff.String()))
	}

	if c.Backup.Keep < 0 {
		problems = append(problems, "backup.keep must be at least 0")
	}
	if c.Backup.FlushTimeout < minBackupFlushTimeout {
		problems = append(problems, fmt.Sprintf("backup.flush_timeout must be at least %s", minBackupFlushTimeout))
	}
	if c.Backup.Interval != 0 && c.Backup.Interval < minBackupInterval {
		problems = append(problems, fmt.Sprintf("backup.interval must be 0 (off) or at least %s", minBackupInterval))
	}
	if c.Backup.World == "." || c.Backup.World == ".." || c.Backup.World != filepath.Base(c.Backup.World) {
		problems = append(problems, "backup.world must be a plain folder name, not a path")
	}
	world := filepath.Join(c.Minecraft.Directory, c.Backup.World)
	if within(world, c.Backup.Directory) {
		problems = append(problems, "backup.directory must not be inside the world folder")
	}

	switch strings.ToLower(strings.TrimSpace(c.Options.LogLevel)) {
	case "debug", "info", "warn", "error":
	default:
		problems = append(problems, fmt.Sprintf(
			"options.log_level (%s) must be one of: debug, info, warn, error",
			c.Options.LogLevel,
		))
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}

	return nil
}

func setDefault[T comparable](target *T, defaultValue T) {
	var zero T
	if *target == zero {
		*target = defaultValue
	}
}

// isLoopbackHost reports whether host only accepts connections from this machine.
func isLoopbackHost(host string) bool {
	host = strings.Trim(host, "[]")
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// within reports whether child is parent or lives under it.
func within(parent, child string) bool {
	p, err1 := filepath.Abs(parent)
	c, err2 := filepath.Abs(child)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(p, c)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
