package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/qivs/agent-deck/internal/opencode"
)

// Record identifies the supervisor that owns a loopback OpenCode server.
type Record struct {
	PID  int `json:"pid"`
	Port int `json:"port"`
}

// Controller manages one private, supervised server without adopting external processes.
type Controller struct {
	Root       string
	Executable string
	OpenCode   string
	OnExit     func(error)
}

func DefaultRoot() (string, error) {
	return StateRoot()
}

func URL(port int) string { return "http://127.0.0.1:" + strconv.Itoa(port) }

func (c Controller) Up(ctx context.Context, port int) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if healthy(ctx, port) {
		return nil
	}
	if record, locked, err := c.inspect(ctx); err != nil {
		return err
	} else if locked {
		if record.Port != port {
			return fmt.Errorf("managed server uses port %d; run agent-deck down first", record.Port)
		}
		return c.waitHealthy(ctx, port)
	}
	var listen net.ListenConfig
	listener, err := listen.Listen(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return fmt.Errorf("port %d is in use by an unresponsive server; choose --port: %w", port, err)
	}
	_ = listener.Close()
	if err := os.MkdirAll(c.Root, 0o700); err != nil {
		return fmt.Errorf("create server state: %w", err)
	}
	return c.launch(ctx, port)
}

func (c Controller) launch(ctx context.Context, port int) error {
	log, err := os.OpenFile(filepath.Join(c.Root, "server.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open server log: %w", err)
	}
	defer func() { _ = log.Close() }()
	command := exec.CommandContext(ctx, c.Executable, "supervise", "--port", strconv.Itoa(port), "--state", c.Root)
	command.Stdout, command.Stderr = log, log
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if c.OpenCode != "" {
		command.Args = append(command.Args, "--opencode", c.OpenCode)
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start supervisor: %w", err)
	}
	if err := command.Process.Release(); err != nil {
		return fmt.Errorf("detach supervisor: %w", err)
	}
	return c.waitHealthy(ctx, port)
}

func healthy(ctx context.Context, port int) bool {
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	return opencode.New(URL(port), true).Health(ctx) == nil
}

func (c Controller) waitHealthy(ctx context.Context, port int) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if healthy(ctx, port) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for server; inspect %s: %w", filepath.Join(c.Root, "server.log"), ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c Controller) inspect(ctx context.Context) (Record, bool, error) {
	if err := ctx.Err(); err != nil {
		return Record{}, false, err
	}
	lock, err := os.OpenFile(filepath.Join(c.Root, "server.lock"), os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("open supervisor lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		return Record{}, false, nil
	} else if !errors.Is(err, syscall.EWOULDBLOCK) {
		return Record{}, false, fmt.Errorf("inspect supervisor lock: %w", err)
	}
	data, err := os.ReadFile(filepath.Join(c.Root, "server.json"))
	if err != nil {
		return Record{}, true, fmt.Errorf("read supervisor identity: %w", err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return record, true, fmt.Errorf("decode supervisor identity: %w", err)
	}
	if record.PID <= 1 || record.Port < 1 || record.Port > 65535 {
		return record, true, fmt.Errorf("invalid supervisor identity")
	}
	return record, true, nil
}

func (c Controller) Status(ctx context.Context, port int) (description string, running bool, err error) {
	record, locked, err := c.inspect(ctx)
	if err != nil {
		return "", false, err
	}
	if locked {
		if healthy(ctx, record.Port) {
			return fmt.Sprintf("running · managed PID %d · %s", record.PID, URL(record.Port)), true, nil
		}
		return fmt.Sprintf("restarting · managed PID %d · %s", record.PID, URL(record.Port)), false, nil
	}
	if healthy(ctx, port) {
		return "running · external (not managed) · " + URL(port), true, nil
	}
	return "stopped · " + URL(port), false, nil
}

func (c Controller) Down(ctx context.Context) error {
	record, locked, err := c.inspect(ctx)
	if err != nil || !locked {
		return err
	}
	if err := syscall.Kill(record.PID, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("stop supervisor: %w", err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		_, locked, err := c.inspect(ctx)
		if err != nil || !locked {
			return err
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for supervisor shutdown: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}
