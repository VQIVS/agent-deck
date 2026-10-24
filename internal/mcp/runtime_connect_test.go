package mcp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestConnectRuntimeOnlyAttachesAndReadOnlyBlocksNotify(t *testing.T) {
	f := newRuntimeFixture(t)
	owner := startTestRuntime(t, f)
	connection, err := ConnectRuntime(t.Context(), f.manager.Team.Name,
		owner.options.JournalPath, owner.options.StateDir, true)
	if err != nil {
		t.Fatal(err)
	}
	if connection.owner != nil || connection.Endpoint() != owner.Endpoint() {
		t.Fatal("attachment started a new owner")
	}
	if _, err := connection.ReadUser(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := connection.Notify(t.Context(), []team.TaskNotice{{To: "bob", Task: "task", Text: "blocked"}}); err == nil {
		t.Fatal("read-only attachment notified")
	}
	connection.Close()
	if !owner.Alive() {
		t.Fatal("attachment close stopped owner")
	}
	_, err = ConnectRuntime(t.Context(), "wrong-team", owner.options.JournalPath, owner.options.StateDir, false)
	if err == nil {
		t.Fatal("cross-team attach accepted")
	}
}

func TestConnectRuntimeValidatesAuthenticatedPingTeam(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
			t.Error("owner credential missing")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"team":"different-live-owner"}`))
	}))
	defer server.Close()
	stateDir := t.TempDir()
	options := RuntimeOptions{StateDir: stateDir, JournalPath: filepath.Join(t.TempDir(), "journal")}
	data := []byte(`{"team":"expected","endpoint":"` + server.URL + `/mcp","token":"` + strings.Repeat("a", 64) + `"}`)
	if err := os.WriteFile(discoveryPath(&options), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ConnectRuntime(t.Context(), "expected", options.JournalPath, stateDir, false); err == nil {
		t.Fatal("trusted discovery team without validating owner ping")
	}
}

func TestConnectRuntimeMissingPreservesNotExistAndWritesNothing(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "missing-state")
	journal := filepath.Join(root, "missing-config", "team.channel.jsonl")
	_, err := ConnectRuntime(t.Context(), "team", journal, state, false)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing owner error lost", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("attachment created state", entries, err)
	}
}

func TestConnectRuntimeRejectsPublicDiscovery(t *testing.T) {
	f := newRuntimeFixture(t)
	owner := startTestRuntime(t, f)
	path := discoveryPath(&owner.options)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ConnectRuntime(t.Context(), f.manager.Team.Name, owner.options.JournalPath, owner.options.StateDir, false)
	if err == nil {
		t.Fatal("public owner credential accepted")
	}
	os.Chmod(path, 0o600)
}
