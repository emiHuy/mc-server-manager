# mc-server-manager

A small Go program that runs a self-hosted Minecraft server and keeps track of it: its state, console output, players, and resource use. It is meant to sit next to one server on a trusted machine.

## What It Does

- Starts the Minecraft server as a child process and stops it gracefully, falling back to a kill if it hangs.
- Tracks the server's state
- Captures console output, keeps recent history, and lets any number of readers follow it live.
- Sends console commands to the server.
- Parses the console into events: ready, player joined, player left, chat, warnings, and errors.
- Tracks which players are online.
- Reports uptime, CPU, memory, and disk usage.
- Runs on Windows and Linux.

## Requirements
 
- Go (see `go.mod` for the version)
- Java, and a Minecraft server jar with its EULA accepted
- Windows or Linux

## Build and run
 
```
go build
```
 
Run the binary from the project root, because the config path is relative to the working directory:
 
```
./mc-server-manager
```
 
During development, `go run .` works too.

## Configuration

Copy `config/config.example.toml` to `config/config.toml` and edit it. The real config file is gitignored.

##  Using It

The manager starts the server immediately and runs until it exits.
 
- **Ctrl+C** asks the server to stop and waits for it to save and exit.
- **A second Ctrl+C** kills the server without a world save.

While running, type into the terminal:
 
| Input | Effect |
|---|---|
| `history` | Print the last 10 console lines. |
| `players` | Print the players currently online. |
| `metrics` | Print uptime, CPU, memory, and disk usage. |
| anything else | Send it to the server as a console command. |
 
These terminal commands are temporary. They will be replaced by a proper CLI and API.
 
If the server exits before it finishes loading, for example because the EULA isn't accepted, the manager reports it as a crash.

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
| `main.go` | Wiring, Ctrl+C handling, and the temporary terminal commands. |
| `config/` | Loads and validates the TOML config, and sets up logging. |
| `minecraft/` | The server instance: process lifecycle, console, events, players, and metrics. |
| `internal/fanout/` | A generic publish and subscribe hub. Slow subscribers lose their oldest items and never block the publisher. |
| `internal/sysmetrics/` | Process CPU and memory, and disk usage. |
 
## Known limitations
 
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
