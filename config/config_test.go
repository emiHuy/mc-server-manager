package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeTOML(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	err := os.WriteFile(path, []byte(content), 0o600)
	require.NoError(t, err)
	return path
}

const baseMinecraftTOML = `
[minecraft]
directory = "/srv/mc"
command = "java"
jar_file = "server.jar"
min_memory = "2G"
max_memory = "4G"
`

func TestLoadConfig_Defaults(t *testing.T) {
	path := writeTOML(t, baseMinecraftTOML)

	cfg, err := loadConfig(path)
	require.NoError(t, err)

	require.Equal(t, defaultHost, cfg.API.Host, "api.host should match default")
	require.Equal(t, defaultPort, cfg.API.Port, "api.port should match default")

	require.Equal(t, defaultStopTimeout, cfg.Minecraft.StopTimeout, "minecraft.stop_timeout should match default")
	require.Equal(t, defaultStartupTimeout, cfg.Minecraft.StartupTimeout, "minecraft.startup_timeout should match default")

	require.Equal(t, defaultMaxRestarts, cfg.AutoRestart.MaxRestarts, "auto_restart.max_restarts should match default")
	require.Equal(t, defaultInitialBackoff, cfg.AutoRestart.InitialBackoff, "auto_restart.initial_backoff should match default")

	require.Equal(t, defaultBackupDirectory, cfg.Backup.Directory, "backup.directory should match default")
	require.Equal(t, defaultBackupWorld, cfg.Backup.World, "backup.world should match default")
	require.Equal(t, defaultBackupFlushTimeout, cfg.Backup.FlushTimeout, "backup.flush_timeout should match default")

	require.Equal(t, defaultLogLevel, cfg.Options.LogLevel, "options.log_level should match default")
}

func TestLogConfig_ValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		toml    string
		wantErr string
	}{
		{
			name:    "missing required minecraft fields",
			toml:    "[api]\nport = 8080\n",
			wantErr: "missing required config keys",
		},
		{
			name:    "stop timeout below minimum",
			toml:    baseMinecraftTOML + "stop_timeout = \"0.5s\"\n",
			wantErr: "minecraft.stop_timeout must be at least",
		},
		{
			name:    "startup timeout below minimum",
			toml:    baseMinecraftTOML + "startup_timeout = \"1s\"\n",
			wantErr: "minecraft.startup_timeout must be at least",
		},
		{
			name:    "invalid api port",
			toml:    baseMinecraftTOML + "[api]\nport = 70000\n",
			wantErr: "api.port must be between 1 and 65535",
		},
		{
			name:    "public host requires token",
			toml:    baseMinecraftTOML + "[api]\nhost = \"0.0.0.0\"\n",
			wantErr: "api.token is required",
		},
		{
			name:    "placeholder token rejected",
			toml:    baseMinecraftTOML + "[api]\ntoken = \"your_generated_token_here\"\n",
			wantErr: "example placeholder",
		},
		{
			name:    "short token rejected",
			toml:    baseMinecraftTOML + "[api]\ntoken = \"too_short\"\n",
			wantErr: "api.token must be at least",
		},
		{
			name:    "negative max restarts",
			toml:    baseMinecraftTOML + "[auto_restart]\nmax_restarts = -1\n",
			wantErr: "auto_restart.max_restarts must be at least 0",
		},
		{
			name:    "auto-restart initial backoff below minimum",
			toml:    baseMinecraftTOML + "[auto_restart]\ninitial_backoff = \"0.5s\"\n",
			wantErr: "auto_restart.initial_backoff must be at least",
		},
		{
			name:    "negative backup keep",
			toml:    baseMinecraftTOML + "[backup]\nkeep = -1\n",
			wantErr: "backup.keep must be at least 0",
		},
		{
			name:    "backup flush timeout below minimum",
			toml:    baseMinecraftTOML + "[backup]\nflush_timeout = \"1s\"\n",
			wantErr: "backup.flush_timeout",
		},
		{
			name:    "backup interval below minimum",
			toml:    baseMinecraftTOML + "[backup]\ninterval = \"1m\"\n",
			wantErr: "backup.interval",
		},
		{
			name:    "backup world cannot be a path",
			toml:    baseMinecraftTOML + "[backup]\nworld = \"../world\"\n",
			wantErr: "plain folder name",
		},
		{
			name:    "backup directory inside world folder rejected",
			toml:    baseMinecraftTOML + "[backup]\ndirectory = \"/srv/mc/world/sub\"\n",
			wantErr: "backup.directory must not be inside",
		},
		{
			name:    "invalid log level",
			toml:    baseMinecraftTOML + "[options]\nlog_level = \"invalid\"\n",
			wantErr: "options.log_level",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadConfig(writeTOML(t, tc.toml))
			require.Error(t, err)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"", true},
		{"localhost", true},
		{"127.0.0.1", true},
		{"[::1]", true},
		{"0.0.0.0", false},
		{TailscaleHost, false},
	}

	for _, tc := range tests {
		got := isLoopbackHost(tc.host)
		require.Equal(t, tc.want, got, fmt.Sprintf("isLoopbackHost(%q) = %v, want %v", tc.host, got, tc.want))
	}
}
