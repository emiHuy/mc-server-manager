# mc-server-manager

A small Go program that runs a self-hosted Minecraft server and keeps track of it: its state, console output, players, and resource use. It exposes everything over an HTTP API. It is meant to sit next to one server on a trusted machine.

## What It Does

- Starts the Minecraft server as a child process and stops it gracefully, falling back to a kill if it hangs.
- Tracks the server's state.
- Captures console output, keeps recent history, and lets any number of readers follow it live.
- Sends console commands to the server.
- Parses the console into events: ready, player joined, player left, chat, warnings, and errors.
- Tracks which players are online.
- Reports uptime, CPU, memory, and disk usage.
- Serves a JSON and Server-Sent Events API, with an optional bearer token.
- Runs on Windows and Linux.

## Requirements
 
- Go (see `go.mod` for the version)
- Java, and a Minecraft server jar with its EULA accepted
- Windows or Linux

## Build and run
 
```
go build
./mc-server-manager
```

During development, `go run .` works too.

## Configuration

Copy `config/config.example.toml` to `config/config.toml` and edit it. The real config file is gitignored.

| Key | Notes |
|---|---|
| `minecraft.autostart` | Start the server when the manager starts. Otherwise use the API. |
| `api.host`, `api.port` | Default `127.0.0.1:8080`. Use `0.0.0.0` to allow other machines. |
| `api.token` | Optional. At least 16 characters. If empty, the API is open. |

Generate a token with `./mc-server-manager token`, then paste it into `[api]` as `token = "..."`. It is case-sensitive.

##  Using It

The manager runs until you press Ctrl+C. It closes the API, then asks the server to stop and waits for it to save and exit. A second Ctrl+C kills the server without a world save.

If the server exits before it finishes loading, for example because the EULA isn't accepted, the manager reports it as a crash.

### API

If a token is set, send `Authorization: Bearer <token>` with every request.

| Method | Path | Description |
|---|---|---|
| GET | `/api/server/status` | State, uptime, and players. |
| POST | `/api/server/start` | Start the server. Returns before the world loads. |
| POST | `/api/server/stop` | Stop the server. Returns once it has exited. |
| POST | `/api/server/command` | Send `{"command": "say hi"}` to the console. |
| GET | `/api/server/console?lines=100` | Recent console lines. |
| GET | `/api/server/metrics` | Uptime, CPU, memory, and disk usage. |
| GET | `/api/server/events` | Live state changes, players, chat, warnings, and errors (SSE). |
| GET | `/api/server/console/stream?lines=N` | Recent lines, then live console output (SSE). |
| GET | `/api/server/metrics/stream` | Live metrics (SSE). |

Errors are JSON: `{"error": "..."}`. An action that doesn't fit the current state, such as stopping a stopped server, returns `409`.

## How it works
 
### State
 
```
stopped -> starting -> running -> stopping -> stopped
                |          |          |
                +----------+----------+--> crashed
```
 
- `starting` lasts until the console prints the "Done" line, not just until the process launches.
- A requested stop always ends as `stopped`, including a stop that had to be killed.
- A clean exit with no stop requested ends as `stopped` if the server was ready, and as `crashed` if it was still loading.
- Any other exit ends as `crashed`.


### Layout
 
| Path | Contents |
|---|---|
| `main.go` | Wiring, the `token` command, and shutdown. |
| `api/` | HTTP handlers, auth, and SSE streaming. |
| `config/` | Loads and validates the TOML config, and sets up logging. |
| `minecraft/` | The server instance: process lifecycle, console, events, players, and metrics. |
| `internal/fanout/` | A generic publish and subscribe hub. Slow subscribers lose their oldest items and never block the publisher. |
| `internal/sysmetrics/` | Process CPU and memory, and disk usage. |
 
## Known limitations
 
- The API is plain HTTP, so the token is visible to anyone sniffing the network. With no token and a non-loopback host, anyone on the network can control the server.
- A slow stream client can miss messages. After reconnecting, fetch `/api/server/status` to resync.
- Readiness detection depends on Minecraft's exact "Done (...)! For help, type "help"" line. It is checked on vanilla only.
- There is no startup timeout. A server that hangs while loading stays in `starting`.
- Chat formats with a prefix, such as `[Not Secure] <name> message`, are not parsed.
- The player list is built from join and leave messages, so it can drift if the manager misses console output.

## Development
 
```
go test ./...
go vet ./...
```
 
CI builds, vets, and tests on Linux and Windows, and also checks formatting and runs the race detector on Linux.

## TODO
 
- `/api/server/restart`
- swagger
- dashboard
- schedule backups
- schedule online/offline hours