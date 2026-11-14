# mc-server-manager

A lightweight manager for a self-hosted Minecraft server. Runs on the host machine and enables remote administration through a web dashboard, CLI, and HTTP API.

![Dashboard](docs/dashboard.png)

## Features

- Control server lifecycle (start, stop, kill)
- Live console and command sending
- Player tracking and events (joins, leaves, chat, warnings, errors)
- CPU, memory, and disk metrics
- Web dashboard, CLI (`mcsm`), and API
- Automatic restart after crashes
- Manual backups with retention (scheduled backups are planned)
- Bearer-token authentication (required when listening beyond localhost)

## Requirements

- Go (see `go.mod` for the version)
- Java, and a Minecraft server jar with its EULA accepted
- Windows or Linux

## Build and run

```
go build
./mc-server-manager
```
Then open the dashboard at <http://127.0.0.1:8080/dashboard> (the default address; see `api.host` and `api.port` below). Press Ctrl+C to stop: the manager asks the server to save and exit, and a second Ctrl+C kills it.

To build the CLI:
```
go build -o mcsm ./cmd/mcsm
```

## Configuration

Copy `config/config.example.toml` to `config/config.toml` and edit it.

| Key | Notes |
|---|---|
| `minecraft.*` | Server directory, jar, and memory settings. |
| `minecraft.autostart` | Start the server when the manager starts. |
| `auto_restart.*` | Restart the server after a crash. Off by default. |
| `api.host`, `api.port` | Default `127.0.0.1:8080`. Set api.host = "tailscale" to automatically bind to your Tailscale IP, or 0.0.0.0 for all interfaces. |
| `api.token` |  At least 16 characters. Required unless `api.host` is a loopback address. You can generate one with `./mc-server-manager token`. |
| `backup.*` | Backup creation and storage. |

## CLI

```
  mcsm status
  mcsm start --wait
  mcsm command say hello
  mcsm logs -f
  mcsm auto-restart true
```
Run `mcsm help` for all commands. Set `MCSM_TOKEN` if the manager requires a token, and `MCSM_ADDR` if it isn't at the default `http://127.0.0.1:8080`.

## API

Interactive docs are served at `/docs`. With a token set, send `Authorization: Bearer <token>`.

## Server states

```
stopped -> starting -> running -> stopping -> stopped
                |          |          |
                +----------+----------+--> crashed
```

`starting` lasts until the console prints its "Done" line.
With `auto_restart` enabled, a crashed server is restarted after a delay that doubles each time, up to 5 minutes, and the manager gives up after `max_restarts` crashes in a row. A server you stop yourself is never restarted. `mcsm auto-restart true|false` changes the setting until the manager restarts.

## Known limitations

- The API is plain HTTP, so the token is visible to anyone sniffing the network. Only expose it on a network you trust, such as a VPN.
- A slow stream client can miss messages. Re-fetch `/api/server/status` after reconnecting.
- Readiness detection expects vanilla's "Done" line.
- A backup waits for vanilla's "Saved the game" line. If it never appears, the backup fails after `backup.flush_timeout` and autosaving is turned back on.
- Backups live on the same machine, and on the same disk unless `backup.directory` points elsewhere, so copy them off the machine if you want protection from disk failure.

## Development

```
go test ./...
go vet ./...
```

## TODO

- Scheduled backups
- Scheduled online/offline hours
- Optional TLS
- Server properties + config editor
- ESP32 server monitoring
