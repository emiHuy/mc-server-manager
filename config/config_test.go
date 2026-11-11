package config

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requiredTOML holds every required key and ends inside the [minecraft]
// table, so tests can append more [minecraft] keys directly.
const requiredTOML = `[minecraft]
directory = "/srv/minecraft"
command = "java"
jar_file = "server.jar"
min_memory = "1G"
max_memory = "2G"
`

func validConfig() Config {
	return Config{
		API: APIConfig{Host: "127.0.0.1", Port: 8080},
		Minecraft: MinecraftConfig{
			Directory:      "/srv/minecraft",
			Command:        "java",
			JarFile:        "server.jar",
			MinMemory:      "1G",
			MaxMemory:      "2G",
			StopTimeout:    30 * time.Second,
			StartupTimeout: 2 * time.Minute,
		},
		AutoRestart: AutoRestartConfig{
			MaxRestarts:    defaultMaxRestarts,
			InitialBackoff: defaultInitialBackoff,
		},
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("cannot write test config: %v", err)
	}
	return path
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(c *Config)
		want   []string // substrings the error must contain; none means no error
	}{
		{
			name:   "valid config",
			mutate: func(c *Config) {},
		},
		{
			name:   "missing directory",
			mutate: func(c *Config) { c.Minecraft.Directory = "" },
			want:   []string{"minecraft.directory"},
		},
		{
			name:   "missing command",
			mutate: func(c *Config) { c.Minecraft.Command = "" },
			want:   []string{"minecraft.command"},
		},
		{
			name:   "missing jar_file",
			mutate: func(c *Config) { c.Minecraft.JarFile = "" },
			want:   []string{"minecraft.jar_file"},
		},
		{
			name:   "missing min_memory",
			mutate: func(c *Config) { c.Minecraft.MinMemory = "" },
			want:   []string{"minecraft.min_memory"},
		},
		{
			name:   "missing max_memory",
			mutate: func(c *Config) { c.Minecraft.MaxMemory = "" },
			want:   []string{"minecraft.max_memory"},
		},
		{
			name: "every required key missing",
			mutate: func(c *Config) {
				c.Minecraft.Directory = ""
				c.Minecraft.Command = ""
				c.Minecraft.JarFile = ""
				c.Minecraft.MinMemory = ""
				c.Minecraft.MaxMemory = ""
			},
			want: []string{
				"minecraft.directory", "minecraft.command", "minecraft.jar_file",
				"minecraft.min_memory", "minecraft.max_memory",
			},
		},
		{
			name:   "stop_timeout of zero",
			mutate: func(c *Config) { c.Minecraft.StopTimeout = 0 },
			want:   []string{"minecraft.stop_timeout"},
		},
		{
			name:   "stop_timeout below the minimum",
			mutate: func(c *Config) { c.Minecraft.StopTimeout = 500 * time.Millisecond },
			want:   []string{"minecraft.stop_timeout"},
		},
		{
			name:   "negative stop_timeout",
			mutate: func(c *Config) { c.Minecraft.StopTimeout = -time.Second },
			want:   []string{"minecraft.stop_timeout"},
		},
		{
			name:   "stop_timeout at the minimum",
			mutate: func(c *Config) { c.Minecraft.StopTimeout = minStopTimeout },
		},
		{
			name:   "startup_timeout of zero",
			mutate: func(c *Config) { c.Minecraft.StartupTimeout = 0 },
			want:   []string{"minecraft.startup_timeout"},
		},
		{
			name:   "startup_timeout below the minimum",
			mutate: func(c *Config) { c.Minecraft.StartupTimeout = 500 * time.Millisecond },
			want:   []string{"minecraft.startup_timeout"},
		},
		{
			name:   "negative startup_timeout",
			mutate: func(c *Config) { c.Minecraft.StartupTimeout = -time.Second },
			want:   []string{"minecraft.startup_timeout"},
		},
		{
			name:   "startup_timeout at the minimum",
			mutate: func(c *Config) { c.Minecraft.StartupTimeout = minStartupTimeout },
		},
		{
			name: "missing key and bad timeout are reported together",
			mutate: func(c *Config) {
				c.Minecraft.Directory = ""
				c.Minecraft.StopTimeout = 0
			},
			want: []string{"minecraft.directory", "minecraft.stop_timeout"},
		},
		{
			name: "optional fields are not checked",
			mutate: func(c *Config) {
				c.Minecraft.Gui = true
				c.Options.LogLevel = "not-a-level"
			},
		},
		{
			name:   "api port too low",
			mutate: func(c *Config) { c.API.Port = -1 },
			want:   []string{"api.port"},
		},
		{
			name:   "api port too high",
			mutate: func(c *Config) { c.API.Port = 70000 },
			want:   []string{"api.port"},
		},
		{
			name:   "api token is optional",
			mutate: func(c *Config) { c.API.Token = "" },
		},
		{
			name:   "token is optional",
			mutate: func(c *Config) { c.API.Token = "" },
		},
		{
			name:   "token at the minimum length",
			mutate: func(c *Config) { c.API.Token = strings.Repeat("a", minTokenLength) },
		},
		{
			name:   "token too short",
			mutate: func(c *Config) { c.API.Token = strings.Repeat("a", minTokenLength-1) },
			want:   []string{"api.token"},
		},
		{
			name:   "placeholder token",
			mutate: func(c *Config) { c.API.Token = placeholderToken },
			want:   []string{"api.token"},
		},
		{
			name:   "max_restarts below 1",
			mutate: func(c *Config) { c.AutoRestart.MaxRestarts = -1 },
			want:   []string{"auto_restart.max_restarts"},
		},
		{
			name:   "initial_backoff below minimum",
			mutate: func(c *Config) { c.AutoRestart.InitialBackoff = minInitialBackoff - time.Millisecond },
			want:   []string{"auto_restart.initial_backoff"},
		},
		{
			name:   "initial_backoff at minimum is valid",
			mutate: func(c *Config) { c.AutoRestart.InitialBackoff = minInitialBackoff },
		},
		{
			name:   "loopback host, no token",
			mutate: func(c *Config) { c.API.Host = "127.0.0.1"; c.API.Token = "" },
		},
		{
			name:   "localhost, no token",
			mutate: func(c *Config) { c.API.Host = "localhost"; c.API.Token = "" },
		},
		{
			name:   "ipv6 loopback, no token",
			mutate: func(c *Config) { c.API.Host = "::1"; c.API.Token = "" },
		},
		{
			name:   "all interfaces, no token",
			mutate: func(c *Config) { c.API.Host = "0.0.0.0"; c.API.Token = "" },
			want:   []string{"api.token"},
		},
		{
			name:   "lan address, no token",
			mutate: func(c *Config) { c.API.Host = "192.168.1.20"; c.API.Token = "" },
			want:   []string{"api.token"},
		},
		{
			name:   "hostname, no token",
			mutate: func(c *Config) { c.API.Host = "mc.example.com"; c.API.Token = "" },
			want:   []string{"api.token"},
		},
		{
			name:   "all interfaces, with token",
			mutate: func(c *Config) { c.API.Host = "0.0.0.0"; c.API.Token = strings.Repeat("a", minTokenLength) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(&c)

			err := c.validate()

			if len(tt.want) == 0 {
				if err != nil {
					t.Fatalf("validate() error: %v, want none", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("validate() returned no error, want one mentioning %v", tt.want)
			}
			for _, key := range tt.want {
				if !strings.Contains(err.Error(), key) {
					t.Errorf("validate() error = %q, want it to mention %q", err, key)
				}
			}
		})
	}
}

// TestValidateNamesOnlyMissingKeys checks the message points at the real
// problem and not at every key.
func TestValidateNamesOnlyMissingKeys(t *testing.T) {
	c := validConfig()
	c.Minecraft.Directory = ""

	err := c.validate()
	if err == nil {
		t.Fatal("validate() returned no error")
	}

	for _, key := range []string{"minecraft.command", "minecraft.jar_file", "minecraft.min_memory", "minecraft.max_memory", "stop_timeout"} {
		if strings.Contains(err.Error(), key) {
			t.Errorf("validate() error = %q, should not mention %q", err, key)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	t.Run("every field is read", func(t *testing.T) {
		path := writeConfig(t, requiredTOML+`gui = true
			stop_timeout = "45s"

			[auto_restart]
			enabled = true
			max_restarts = 3
			initial_backoff = "10s"

			[options]
			log_level = "debug"
		`)

		conf, err := loadConfig(path)
		if err != nil {
			t.Fatalf("loadConfig() error: %v", err)
		}

		m := conf.Minecraft
		if m.Directory != "/srv/minecraft" || m.Command != "java" || m.JarFile != "server.jar" {
			t.Errorf("paths = %q, %q, %q", m.Directory, m.Command, m.JarFile)
		}
		if m.MinMemory != "1G" || m.MaxMemory != "2G" {
			t.Errorf("memory = %q, %q, want 1G, 2G", m.MinMemory, m.MaxMemory)
		}
		if !m.Gui {
			t.Error("Gui = false, want true")
		}
		if m.StopTimeout != 45*time.Second {
			t.Errorf("StopTimeout = %v, want 45s", m.StopTimeout)
		}
		if conf.Options.LogLevel != "debug" {
			t.Errorf("LogLevel = %q, want %q", conf.Options.LogLevel, "debug")
		}
		if conf.API.Host != defaultHost || conf.API.Port != defaultPort {
			t.Errorf("API = %s:%d, want the defaults %s:%d", conf.API.Host, conf.API.Port, defaultHost, defaultPort)
		}
		if conf.API.Token != "" {
			t.Errorf("Token = %q, want empty when omitted", conf.API.Token)
		}
		if conf.Minecraft.Autostart {
			t.Error("Autostart = true, want false when omitted")
		}

		a := conf.AutoRestart
		if !a.Enabled {
			t.Error("Enabled = false, want true")
		}
		if a.MaxRestarts != 3 {
			t.Errorf("MaxRestarts = %d, want 3", a.MaxRestarts)
		}
		if a.InitialBackoff != 10*time.Second {
			t.Errorf("InitialBackoff = %v, want 10s", a.InitialBackoff)
		}
	})

	t.Run("optional fields get defaults", func(t *testing.T) {
		path := writeConfig(t, requiredTOML)

		conf, err := loadConfig(path)
		if err != nil {
			t.Fatalf("loadConfig() error: %v", err)
		}

		if conf.Minecraft.Gui {
			t.Error("Gui = true, want false when omitted")
		}
		if conf.Minecraft.StopTimeout != defaultStopTimeout {
			t.Errorf("StopTimeout = %v, want the default %v", conf.Minecraft.StopTimeout, defaultStopTimeout)
		}
		if conf.Options.LogLevel != "" {
			t.Errorf("LogLevel = %q, want empty when omitted", conf.Options.LogLevel)
		}

		a := conf.AutoRestart
		if a.Enabled {
			t.Error("Enabled = true, want false when omitted")
		}
		if a.MaxRestarts != defaultMaxRestarts {
			t.Errorf("MaxRestarts = %d, want the default %d", a.MaxRestarts, defaultMaxRestarts)
		}
		if a.InitialBackoff != defaultInitialBackoff {
			t.Errorf("InitialBackoff = %v, want the default %v", a.InitialBackoff, defaultInitialBackoff)
		}
	})

	t.Run("stop_timeout values", func(t *testing.T) {
		tests := []struct {
			value string
			want  time.Duration
		}{
			{`"2m"`, 2 * time.Minute},
			{`"1m30s"`, 90 * time.Second},
			{`"1s"`, time.Second},
			// Zero is indistinguishable from omitted, so it takes the default.
			{`"0s"`, defaultStopTimeout},
		}

		for _, tt := range tests {
			path := writeConfig(t, requiredTOML+"stop_timeout = "+tt.value+"\n")

			conf, err := loadConfig(path)
			if err != nil {
				t.Errorf("stop_timeout = %s: loadConfig() error: %v", tt.value, err)
				continue
			}
			if conf.Minecraft.StopTimeout != tt.want {
				t.Errorf("stop_timeout = %s: StopTimeout = %v, want %v", tt.value, conf.Minecraft.StopTimeout, tt.want)
			}
		}
	})

	t.Run("zero max_restarts takes the default", func(t *testing.T) {
		path := writeConfig(t, requiredTOML+"\n[auto_restart]\nmax_restarts = 0\n")

		conf, err := loadConfig(path)
		if err != nil {
			t.Fatalf("loadConfig() error: %v", err)
		}
		if conf.AutoRestart.MaxRestarts != defaultMaxRestarts {
			t.Errorf("MaxRestarts = %d, want the default %d", conf.AutoRestart.MaxRestarts, defaultMaxRestarts)
		}
	})

	t.Run("too-small initial_backoff is rejected", func(t *testing.T) {
		path := writeConfig(t, requiredTOML+"\n[auto_restart]\ninitial_backoff = \"1ms\"\n")

		_, err := loadConfig(path)
		if err == nil || !strings.Contains(err.Error(), "auto_restart.initial_backoff") {
			t.Errorf("loadConfig() error = %v, want it to name auto_restart.initial_backoff", err)
		}
	})
}

func TestLoadConfigErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string // substrings the error must contain; {path} is the file path
	}{
		{
			name:    "missing required key",
			content: strings.Replace(requiredTOML, `directory = "/srv/minecraft"`+"\n", "", 1),
			want:    []string{"minecraft.directory"},
		},
		{
			name:    "empty required value",
			content: strings.Replace(requiredTOML, `jar_file = "server.jar"`, `jar_file = ""`, 1),
			want:    []string{"minecraft.jar_file"},
		},
		{
			name:    "stop_timeout below the minimum",
			content: requiredTOML + `stop_timeout = "500ms"` + "\n",
			want:    []string{"minecraft.stop_timeout"},
		},
		{
			name:    "stop_timeout without a unit",
			content: requiredTOML + `stop_timeout = "30"` + "\n",
			want:    []string{"{path}"},
		},
		{
			name:    "stop_timeout in words",
			content: requiredTOML + `stop_timeout = "30 seconds"` + "\n",
			want:    []string{"{path}"},
		},
		{
			// An unquoted integer reads as nanoseconds, which is below the
			// minimum. Either layer may reject it.
			name:    "stop_timeout as a bare number",
			content: requiredTOML + "stop_timeout = 30\n",
		},
		{
			name:    "syntax error",
			content: "[minecraft\ndirectory =",
			want:    []string{"{path}"},
		},
		{
			name:    "key with no value",
			content: requiredTOML + "gui =\n",
			want:    []string{"{path}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeConfig(t, tt.content)

			_, err := loadConfig(path)
			if err == nil {
				t.Fatal("loadConfig() returned no error")
			}

			for _, want := range tt.want {
				want = strings.ReplaceAll(want, "{path}", path)
				if !strings.Contains(err.Error(), want) {
					t.Errorf("loadConfig() error = %q, want it to contain %q", err, want)
				}
			}
		})
	}

	t.Run("file does not exist", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.toml")

		_, err := loadConfig(path)
		if err == nil {
			t.Fatal("loadConfig() returned no error")
		}
		if !strings.Contains(err.Error(), path) {
			t.Errorf("loadConfig() error = %q, want it to mention %q", err, path)
		}
	})
}

func TestInitLoggerLevels(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	tests := []struct {
		name  string
		level string
		debug bool
		info  bool
		warn  bool
		err   bool
	}{
		{"debug", "debug", true, true, true, true},
		{"info", "info", false, true, true, true},
		{"warn", "warn", false, false, true, true},
		{"error", "error", false, false, false, true},
		{"empty falls back to info", "", false, true, true, true},
		{"upper case is accepted", "DEBUG", true, true, true, true},
		{"unknown falls back to info", "verbose", false, true, true, true},
	}

	ctx := context.Background()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Config{Options: Options{LogLevel: tt.level}}
			c.InitLogger()

			logger := slog.Default()
			checks := []struct {
				level slog.Level
				want  bool
			}{
				{slog.LevelDebug, tt.debug},
				{slog.LevelInfo, tt.info},
				{slog.LevelWarn, tt.warn},
				{slog.LevelError, tt.err},
			}

			for _, check := range checks {
				if got := logger.Enabled(ctx, check.level); got != check.want {
					t.Errorf("level %q: Enabled(%v) = %v, want %v", tt.level, check.level, got, check.want)
				}
			}
		})
	}
}

func TestAPIConfigIsLoopback(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{"empty defaults to loopback", "", true},
		{"localhost", "localhost", true},
		{"localhost any case", "LOCALHOST", true},
		{"ipv4 loopback", "127.0.0.1", true},
		{"other 127 address", "127.0.0.5", true},
		{"ipv6 loopback", "::1", true},
		{"bracketed ipv6 loopback", "[::1]", true},
		{"all ipv4 interfaces", "0.0.0.0", false},
		{"all ipv6 interfaces", "::", false},
		{"lan address", "192.168.1.20", false},
		{"tailscale placeholder", "tailscale", false},
		{"hostname", "example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isLoopbackHost(tt.host)
			if got != tt.want {
				t.Errorf("APIConfig{Host: %q}.IsLoopback() = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}
