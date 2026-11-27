package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"time"
)

const (
	defaultAddr = "http://127.0.0.1:8080"

	quickTimeout   = 15 * time.Second
	startTimeout   = 5 * time.Minute
	stopTimeout    = 2 * time.Minute
	restartTimeout = stopTimeout + 10*time.Second
	backupTimeout  = 10 * time.Minute
)

const usageText = `usage: mcsm [--addr URL] [--token TOKEN] <command>

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
`

type usageError string

func (e usageError) Error() string {
	return string(e)
}

func main() {
	addr := flag.String("addr", envOr("MCSM_ADDR", defaultAddr), "manager address")
	token := flag.String("token", os.Getenv("MCSM_TOKEN"), "API token (prefer MCSM_TOKEN)")

	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	c := &client{base: strings.TrimRight(*addr, "/"), token: *token}
	err := run(ctx, c, args[0], args[1:])
	stop()

	var ue usageError
	switch {
	case err == nil:
	case errors.As(err, &ue):
		fmt.Fprintln(os.Stderr, "mcsm:", err)
		usage()
		os.Exit(2)
	default:
		fmt.Fprintln(os.Stderr, "mcsm:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, c *client, cmd string, args []string) error {
	switch cmd {
	case "status":
		return c.status(ctx)
	case "metrics":
		return c.metrics(ctx)
	case "players":
		return c.players(ctx)
	case "start":
		return c.start(ctx, args)
	case "stop":
		return c.action(ctx, "stop", stopTimeout)
	case "kill":
		return c.action(ctx, "kill", quickTimeout)
	case "restart":
		return c.action(ctx, "restart", restartTimeout)
	case "command":
		if len(args) == 0 {
			return usageError("command needs some text")
		}
		return c.command(ctx, strings.Join(args, " "))
	case "logs":
		return c.logs(ctx, args)
	case "auto-restart":
		return c.autoRestart(ctx, args)
	case "backup":
		return c.backup(ctx, args)
	case "help":
		usage()
		return nil
	default:
		return usageError("unknown command " + cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, usageText)
}

func envOr(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
