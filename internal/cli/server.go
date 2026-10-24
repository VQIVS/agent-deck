package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/qivs/agent-deck/internal/server"
)

func serverCommand(action string, args []string) error {
	f := flag.NewFlagSet(action, flag.ContinueOnError)
	port := f.Int("port", 4096, "loopback OpenCode port")
	root := f.String("state", server.DefaultRoot(), "private supervisor state directory")
	opencode := f.String("opencode", "opencode", "OpenCode executable")
	if err := f.Parse(args); err != nil {
		return fmt.Errorf("parse server flags: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find agent-deck executable: %w", err)
	}
	controller := server.Controller{Root: *root, Executable: executable, OpenCode: *opencode}
	controller.OnExit = func(err error) { fmt.Fprintln(os.Stderr, time.Now().UTC().Format(time.RFC3339), err) }
	if action == "supervise" {
		ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
		defer cancel()
		go superviseTeams(ctx)
		if err := controller.Supervise(ctx, *port); err != nil {
			return fmt.Errorf("supervise local server: %w", err)
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	switch action {
	case "up":
		if err := controller.Up(ctx, *port); err != nil {
			return fmt.Errorf("start local server: %w", err)
		}
	case "down":
		if err := controller.Down(ctx); err != nil {
			return fmt.Errorf("stop local server: %w", err)
		}
		fmt.Println("Managed server stopped; external servers are retained.")
		return nil
	}
	status, running, err := controller.Status(ctx, *port)
	if err != nil {
		return fmt.Errorf("inspect local server: %w", err)
	}
	fmt.Println(status)
	if !running {
		return fmt.Errorf("server is not ready; run agent-deck up --port %d", *port)
	}
	return nil
}
