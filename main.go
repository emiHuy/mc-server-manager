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
		slog.Error("failed to load config", "error", err)
		return
	}

	conf.InitLogger()

	slog.Info("application starting")
	slog.Debug("configuration loaded")

	instance := minecraft.New(conf.Minecraft)

	eventSub := instance.SubscribeEvents()
	eventsPrinted := make(chan struct{})
	go func() {
		defer close(eventsPrinted)
		for e := range eventSub.Events {
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

	_, sub := instance.Subscribe(0)
	printed := make(chan struct{})
	go func() {
		defer close(printed)
		for line := range sub.Lines {
			fmt.Println(line)
		}
	}()
	defer func() {
		sub.Cancel()
		<-printed
	}()

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "history" {
				for _, l := range instance.RecentConsole(10) {
					fmt.Println(l)
				}
			} else {
				err := instance.Send(line)
				if err != nil {
					slog.Error("send failed", "error", err)
				}
			}
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	done := instance.Done()

	select {
	case <-done:
		return
	case <-sig:
		slog.Info("interrupt received")
	}

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
			slog.Error("minecraft server kill request failed", "error", err)
		}
	}

	select {
	case err := <-stopResult:
		logStopResult(err, true)
	case <-time.After(killWait):
		slog.Error("minecraft server did not confirm exit after kill", "timeout", killWait.String())
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
