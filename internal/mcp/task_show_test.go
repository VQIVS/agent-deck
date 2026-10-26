package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestShowTaskAuthenticatedFullDetailsWithoutMutation(t *testing.T) {
	tools, _ := taskToolsFixture(t)
	manager, store, err := tools.Provider(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	manager.Team.Members[0].ControlDir = "/private/control-must-not-leak"
	tools.Provider = func(context.Context) (*team.Manager, team.BoardStore, error) { return manager, store, nil }
	_, err = callTask(t, tools, "lead", "create_task", `{"title":"Detailed task","description":"Full requirements",`+
		`"criteria":["first","second"],"paths":["src/**"],"reviewer":"rev","assignee":"ana"}`)
	if err != nil {
		t.Fatal(err)
	}
	for range 7 {
		if _, err := callTask(t, tools, "lead", "comment_task", `{"id":"T-1","text":"retained discussion"}`); err != nil {
			t.Fatal(err)
		}
	}
	boardPath := filepath.Join(store.Root, store.Team, "board.json")
	before, err := os.ReadFile(boardPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, readonly := range []bool{false, true} {
		manager.ReadOnly = readonly
		for _, member := range []string{"lead", "ana", "rev"} {
			result, err := callTask(t, tools, member, "show_task", `{"id":"T-1"}`)
			if err != nil {
				t.Fatalf("member=%s readonly=%v: %v", member, readonly, err)
			}
			detail := result.(taskDetail)
			if detail.Description != "Full requirements" || len(detail.Criteria) != 2 || len(detail.Comments) != 7 ||
				len(detail.History) < 8 || detail.Assignee != "ana" || detail.Reviewer != "rev" || len(detail.Paths) != 1 {
				t.Fatalf("incomplete task detail: %+v", detail)
			}
			encoded, err := json.Marshal(result)
			if err != nil || bytes.Contains(encoded, []byte("control-must-not-leak")) {
				t.Fatalf("invalid/private detail response: %s %v", encoded, err)
			}
		}
	}
	after, err := os.ReadFile(boardPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("detail read mutated persisted board: %v", err)
	}
	if _, err := tools.Call(t.Context(), Identity{Team: "another", Member: "ana"}, "show_task",
		json.RawMessage(`{"id":"T-1"}`)); err == nil {
		t.Fatal("cross-team read accepted")
	}
	for _, input := range []string{`{}`, `{"id":"absent"}`, `{"id":"T-1","user":true}`} {
		if _, err := callTask(t, tools, "ana", "show_task", input); err == nil {
			t.Fatal("invalid detail request accepted", input)
		}
	}
	if _, err := callTask(t, tools, "stranger", "show_task", `{"id":"T-1"}`); err == nil {
		t.Fatal("nonmember read accepted")
	}
	manager.ReadOnly = false
	if _, err := callTask(t, tools, "rev", "update_my_task", `{"id":"T-1","checked":["c1"]}`); err == nil {
		t.Fatal("detail visibility granted mutation authority")
	}
	manager.Team.Members[1].Muted = true
	if _, err := callTask(t, tools, "ana", "show_task", `{"id":"T-1"}`); err == nil {
		t.Fatal("muted identity read accepted")
	}
}

func TestShowTaskRejectsLegacyWithoutWriting(t *testing.T) {
	store := team.BoardStore{Root: t.TempDir(), Team: "legacy"}
	directory := filepath.Join(store.Root, store.Team)
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"version":1,"revision":1,"board":{"version":1,"tasks":[` +
		`{"id":"old","title":"Historical","state":"pending","paths":["src/**"],"estimate":{}}]},` +
		`"policy":{"approval":"off"},"approved":{}}`)
	path := filepath.Join(directory, "board.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := readShowTask(t.Context(), store, "old")
	if err == nil || !strings.Contains(err.Error(), "board v1 unsupported") {
		t.Fatal("unclear legacy board error", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(data, after) {
		t.Fatal("read persisted legacy migration", err)
	}
	_, err = readShowTask(t.Context(), store, "missing")
	if err == nil || !strings.Contains(err.Error(), "board v1 unsupported") {
		t.Fatal("unclear legacy board error", err)
	}
}

func TestShowTaskSchemaAndRuntimeRegistration(t *testing.T) {
	tools, _ := taskToolsFixture(t)
	found := 0
	for _, definition := range tools.Definitions() {
		if definition["name"] == "show_task" {
			found++
			encoded, _ := json.Marshal(definition)
			if !bytes.Contains(encoded, []byte(`"required":["id"]`)) {
				t.Fatal("schema does not require ID")
			}
		}
	}
	if found != 1 || !IsTaskTool("show_task") {
		t.Fatal("show_task missing or duplicated", found)
	}
	server := &Server{Tasks: tools}
	_, handled, err := server.callWorkflow(t.Context(), Identity{Team: "pool", Member: "ana"},
		"show_task", json.RawMessage(`{"id":"absent"}`))
	if !handled || err == nil || !strings.Contains(err.Error(), "unknown task") {
		t.Fatal("runtime routing bypassed task read", handled, err)
	}
}
