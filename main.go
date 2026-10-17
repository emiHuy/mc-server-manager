package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/emiHuy/mc-server-manager/config"
	"github.com/emiHuy/mc-server-manager/minecraft"
)

const killWait = 5 * time.Second

func main() {
	conf, err := config.LoadConfig()
	if err != nil {
		slog.Error("config load failed", "error", err)
		return
	}

	conf.InitLogger()

	slog.Info("application starting")
	slog.Debug("configuration loaded")

	instance := minecraft.New(conf.Minecraft)

	// Temporary scaffolding until the CLI and API exist: print events and console output, and read commands from stdin.
	eventSub := instance.SubscribeEvents()
	eventsPrinted := make(chan struct{})
	go func() {
		defer close(eventsPrinted)
		for e := range eventSub.C {
			if e.Type == minecraft.EventStateChanged {
				fmt.Printf("[event] %s -> %s\n", e.Type, e.State)
			} else {
				fmt.Printf("[event] %s %q\n", e.Type, e.Message)
			}
		}
	}()
	defer func() {
		eventSub.Cancel()
		<-eventsPrinted
	}()

	err = instance.Start()
	if err != nil {
		slog.Error("minecraft server start failed", "error", err)
		return
	}

	_, sub := instance.SubscribeConsole(0)
	printed := make(chan struct{})
	go func() {
		defer close(printed)
		for line := range sub.C {
			fmt.Println(line)
		}
	}()
	defer func() {
		sub.Cancel()
		<-printed
	}()

	// These words are handled by the manager.
	// Anything else is sent to the server as a console command.
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			switch line {
			case "history":
				for _, l := range instance.RecentConsole(10) {
					fmt.Println(l)
				}
			case "players":
				fmt.Println(instance.Players())
			case "metrics":
				m, err := instance.Metrics()
				if err != nil {
					slog.Warn("metrics failed", "error", err)
				}
				fmt.Printf("%+v\n", m)
			default:
				err := instance.Send(line)
				if err != nil {
					slog.Warn("send failed", "error", err)
				}
			}
		}

		err := scanner.Err()
		if err != nil {
			slog.Error("stdin read failed", "error", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	done := instance.Done()

	// If the server exits on its own, the instance has already logged how.
	select {
	case <-done:
		return
	case <-sig:
		slog.Info("interrupt received")
	}

	// Stop blocks until the server exits, so run it in a goroutine and keep
	// listening: a second interrupt kills the server instead of waiting.
	stopResult := make(chan error, 1)
	go func() {
		stopResult <- instance.Stop()
	}()

	select {
	case err := <-stopResult:
		logStopResult(err, false)
		return
	case <-sig:
		slog.Warn("second interrupt received, killing minecraft server")
		err = instance.Kill()
		if err != nil {
			slog.Error("minecraft server kill failed", "error", err)
		}
	}

	// Kill makes the pending Stop return, but bound the wait so a process that will not die cannot hang the manager.
	select {
	case err := <-stopResult:
		logStopResult(err, true)
	case <-time.After(killWait):
		slog.Error("minecraft server exit not confirmed after kill", "timeout", killWait.String())
	}
}

func logStopResult(err error, forced bool) {
	switch {
	case err != nil:
		slog.Error("minecraft server stop failed", "error", err)
	case forced:
		slog.Info("minecraft server stopped after kill")
	default:
		slog.Info("minecraft server stopped gracefully")
	}
}
