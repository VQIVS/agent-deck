package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestExternalServerAndStaleRecord(t *testing.T) {
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"healthy":true}`)
	}))
	defer host.Close()
	u, _ := url.Parse(host.URL)
	port, _ := strconv.Atoi(u.Port())
	c := Controller{Root: t.TempDir()}
	if err := c.Up(t.Context(), port); err != nil {
		t.Fatal(err)
	}
	if status, ok, err := c.Status(t.Context(), port); err != nil || !ok || !strings.Contains(status, "external") {
		t.Fatalf("%s %t %v", status, ok, err)
	}
	if err := c.Down(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !healthy(t.Context(), port) {
		t.Fatal("external server stopped")
	}
	if err := os.WriteFile(filepath.Join(c.Root, "server.json"), []byte(`{"pid":1,"port":4096}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := c.Down(t.Context()); err != nil {
		t.Fatal("stale PID should never be signaled", err)
	}
	if err := c.Up(t.Context(), 0); err == nil {
		t.Fatal("invalid port accepted")
	}
}

func TestSupervisorHelper(t *testing.T) {
	if os.Getenv("AGENT_DECK_TEST_SUPERVISOR") != "1" {
		return
	}
	ctx, cancel := signal.NotifyContext(t.Context(), syscall.SIGTERM)
	defer cancel()
	c := Controller{Root: os.Getenv("AGENT_DECK_TEST_STATE"), OpenCode: os.Getenv("AGENT_DECK_TEST_CHILD")}
	if err := c.Supervise(ctx, 49191); err != nil {
		t.Fatal(err)
	}
}

func TestCrashRestartAndShutdown(t *testing.T) {
	root := t.TempDir()
	count := filepath.Join(root, "starts")
	child := filepath.Join(root, "opencode")
	script := "#!/bin/sh\nprintf 'start\\n' >> '" + count + "'\nexit 1\n"
	if err := os.WriteFile(child, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSupervisorHelper$")
	command.Env = append(os.Environ(), "AGENT_DECK_TEST_SUPERVISOR=1", "AGENT_DECK_TEST_STATE="+root,
		"AGENT_DECK_TEST_CHILD="+child)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	for {
		data, _ := os.ReadFile(count)
		if strings.Count(string(data), "start") >= 2 {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("crashed child was not restarted")
		case <-time.After(50 * time.Millisecond):
		}
	}
	data, err := os.ReadFile(filepath.Join(root, "server.json"))
	if err != nil {
		t.Fatal(err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil || record.PID != command.Process.Pid {
		t.Fatalf("invalid owner: %s %v", data, err)
	}
	c := Controller{Root: root}
	if err := c.Up(ctx, 49192); err == nil {
		t.Fatal("second managed port accepted")
	}
	if err := c.Down(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "server.json")); !os.IsNotExist(err) {
		t.Fatal("PID record retained after shutdown")
	}
	_ = syscall.Kill(command.Process.Pid, syscall.SIGTERM)
}
