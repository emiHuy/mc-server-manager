# Configuration

Copy `config/config.example.toml` to `config/config.toml` and edit it.

The manager reads `./config/config.toml` relative to the directory it is launched from, so start it from the folder that contains `config/`.

On Windows, write paths in single quotes (`'C:\path\to\server'`) so backslashes are not treated as escapes.

## `[api]`

| Key | Default | Notes |
|---|---|---|
| `host` | `127.0.0.1` | `127.0.0.1` for this machine only, `0.0.0.0` for all interfaces, or `tailscale` to bind to this machine's Tailscale IP. |
| `port` | `8080` | |
| `token` | none | At least 16 characters. Optional on a loopback host, **required for any other host** (`0.0.0.0`, `tailscale`, or a specific address). Generate one with `./mc-server-manager token`. |

## `[minecraft]`

| Key | Required | Default | Notes |
|---|---|---|---|
| `directory` | yes | | Folder with the server jar, world, and `eula.txt`. |
| `command` | yes | | Java executable. On Windows use the full path to `java.exe`. |
| `jar_file` | yes | | Jar name inside the directory. |
| `min_memory`, `max_memory` | yes | | Java heap, e.g. `"4G"`. |
| `gui` | no | `false` | Show Minecraft's own server window. |
| `stop_timeout` | no | `"30s"` | Grace period for a clean stop before force-kill. Needs a unit (`s`, `m`); minimum `1s`. |
| `startup_timeout` | no | | How long to wait for the server to finish loading before treating the start as failed. Needs a unit (`s`, `m`); minimum `5s`. |
| `autostart` | no | `false` | Start the server when the manager starts. |

## `[auto_restart]`

| Key | Notes |
|---|---|
| `enabled` | Restart the server if it crashes. A deliberate `stop` or `kill` is never restarted. |
| `max_restarts` | Give up after this many attempts. |
| `initial_backoff` | Delay before the first restart. Later attempts wait longer.  Needs a unit (`s`, `m`); minimum `1s`. |

## `[backup]`

| Key | Notes |
|---|---|
| `directory` | Where backups are written. |
| `keep` | Number of newest backups to keep; older ones are deleted. |
| `world` | World folder inside `minecraft.directory`. |
| `flush_timeout` | Maximum wait for the server's "Save" confirmation before the backup.  Needs a unit (`s`, `m`); minimum `5s`. |
| `interval` | Optional. Back up automatically this often, e.g. `"24h"`. Omit it or set `0` to turn automatic backups off. Needs a unit (`s`, `m`); minimum `5m`. |

## `[options]`

| Key | Notes |
|---|---|
| `log_level` | `debug`, `info`, `warn`, or `error`. |
