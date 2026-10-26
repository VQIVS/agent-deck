package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/vqivs/agent-deck/internal/fsutil"
)

func (c Controller) Supervise(ctx context.Context, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid server port")
	}
	if err := os.MkdirAll(c.Root, 0o700); err != nil {
		return fmt.Errorf("create supervisor state: %w", err)
	}
	release, err := fsutil.Lock(ctx, filepath.Join(c.Root, "server.lock"), false)
	if err != nil {
		return fmt.Errorf("open supervisor lock: %w", err)
	}
	defer release()
	if err := c.writeRecord(ctx, Record{PID: os.Getpid(), Port: port}); err != nil {
		return err
	}
	defer func() { _ = os.Remove(filepath.Join(c.Root, "server.json")) }()
	return c.restart(ctx, port)
}

func (c Controller) writeRecord(ctx context.Context, record Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode supervisor identity: %w", err)
	}
	if err := fsutil.WriteAtomic(ctx, filepath.Join(c.Root, "server.json"), data, 0o600, ".server-*"); err != nil {
		return fmt.Errorf("publish supervisor identity: %w", err)
	}
	return nil
}

func (c Controller) restart(ctx context.Context, port int) error {
	delay := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := c.runChild(ctx, port)
		if c.OnExit != nil {
			c.OnExit(err)
		}
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(started) > time.Minute {
			delay = time.Second
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(delay):
		}
		delay = min(30*time.Second, delay*2)
	}
	return nil
}

func (c Controller) runChild(ctx context.Context, port int) error {
	name := c.OpenCode
	if name == "" {
		name = "opencode"
	}
	command := exec.CommandContext(ctx, name, "serve", "--hostname", "127.0.0.1", "--port", strconv.Itoa(port))
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if err := syscall.Kill(-command.Process.Pid, syscall.SIGTERM); err != nil {
			return fmt.Errorf("terminate server process group: %w", err)
		}
		return nil
	}
	command.WaitDelay = 5 * time.Second
	if err := command.Run(); err != nil {
		return fmt.Errorf("run OpenCode: %w", err)
	}
	return nil
}
