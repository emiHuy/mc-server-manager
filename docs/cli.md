# CLI

## Command List

```
usage: mcsm [--addr URL] [--token TOKEN] <command>

commands:
	status                   state, uptime, players, auto-restart
	metrics                  CPU, memory, and disk usage
	players                  list online players
	start [--wait]           start the server (--wait blocks until it is running)
	stop                     stop gracefully (blocks until server has exited)
	kill                     kill immediately, without a world save
	command <text...>        send a console command
	logs [-n N] [-f]         recent console lines; -f follows live output
	auto-restart true|false  restart automatically after a crash (until the manager restarts)
	backup create            back up the world (waits until it finishes)
	backup list              list backups, newest first
	backup delete <id>       delete a backup

environment:
	MCSM_ADDR            manager address (default ` + defaultAddr + `)
	MCSM_TOKEN           API token (preferred over --token, which shows in ps)
```

## Examples

```
mcsm start --wait
mcsm command say hello
mcsm logs -n 50
mcsm logs -f
```

## Environment Variables

| Variable | Purpose |
|---|---|
| `MCSM_TOKEN` | API token. Preferred over `--token`, which shows up in process listings. |
| `MCSM_ADDR` | Manager address if it isn't at the default `http://127.0.0.1:8080`. |

Global flags can be used in place of environment variables and go before the command: `mcsm --addr http://host:8080 status`. However, environment variables are preferred.

Run `mcsm help` for the command list.
