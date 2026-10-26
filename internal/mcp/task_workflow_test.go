package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestWorkflowToolSchemasDoNotEnumerateLegacyStages(t *testing.T) {
	tools := (&TaskTools{}).Definitions()
	for _, definition := range tools {
		name, _ := definition["name"].(string)
		if name != "move_task" && name != "update_my_task" {
			continue
		}
		encoded, err := json.Marshal(definition)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), `"enum"`) {
			t.Fatal("configured stages limited by schema", string(encoded))
		}
		if name == "move_task" && !strings.Contains(string(encoded), `"version"`) {
			t.Fatal("shipping version missing")
		}
	}
}

func TestWorkflowDiscoveryReadonlyAndCrossTeamIdentity(t *testing.T) {
	tools, _ := taskToolsFixture(t)
	provider := tools.Provider
	tools.Provider = func(ctx context.Context) (*team.Manager, team.BoardStore, error) {
		manager, store, err := provider(ctx)
		manager.ReadOnly = true
		return manager, store, err
	}
	if _, err := tools.Call(t.Context(), Identity{Team: "other", Member: "ana"},
		"task_workflow", json.RawMessage(`{}`)); err == nil {
		t.Fatal("cross-team discovery accepted")
	}
	if _, err := tools.Call(t.Context(), Identity{Team: "pool", Member: "ana"},
		"task_workflow", json.RawMessage(`{}`)); err != nil {
		t.Fatal("read-only discovery blocked", err)
	}
	if _, err := tools.Call(t.Context(), Identity{Team: "pool", Member: "lead"}, "move_task",
		json.RawMessage(`{"id":"T-1","state":"released","version":"v1"}`)); err == nil {
		t.Fatal("read-only release accepted")
	}
}

func TestCustomWorkflowAuthenticatedReleaseAndDiscovery(t *testing.T) {
	definition := team.DefaultWorkflow()
	for i := range definition.Stages {
		if definition.Stages[i].Name == "done" {
			definition.Stages[i].Name = "Accepted"
		}
		if definition.Stages[i].Name == "released" {
			definition.Stages[i].Name = "Published"
		}
	}
	definition.Transitions = map[string]team.WorkflowTransition{
		"Published": {From: []string{"Accepted"}, By: []string{"release-manager", "user"}},
	}
	manager := &team.Manager{Team: team.Team{Name: "custom", Workflow: definition, Members: []team.Member{
		{Name: "lead", Role: "master"}, {Name: "release-manager", Role: "worker"}, {Name: "builder", Role: "worker"},
	}}}
	store := team.BoardStore{Root: t.TempDir(), Team: "custom"}
	completed := time.Now().UTC().Add(-time.Hour)
	state := team.BoardState{Version: 2, Board: team.TaskBoard{Version: 2, Workflow: definition, Tasks: []team.Task{{
		ID: "T-1", Title: "accepted work", State: "Accepted", LegacyCompletion: true, CompletedAt: completed,
		Criteria: []team.TaskCriterion{{ID: "c1", Text: "works"}}, Paths: team.Leases{"own/**"},
	}}}, Policy: team.SchedulingPolicy{Approval: "off"}}
	if err := store.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	acceptedNotices := team.TaskNotices(&team.TaskBoard{Version: 2, Workflow: definition}, &state.Board,
		&manager.Team, team.TaskActor{User: true})
	if !slices.ContainsFunc(acceptedNotices, func(notice team.TaskNotice) bool {
		return notice.To == "release-manager" && notice.Event != "" && notice.Task == "T-1"
	}) {
		t.Fatal("custom done category did not queue release-manager", acceptedNotices)
	}
	var notices []team.TaskNotice
	tools := &TaskTools{
		Provider: func(context.Context) (*team.Manager, team.BoardStore, error) { return manager, store, nil },
		Notify:   func(_ context.Context, sent []team.TaskNotice) error { notices = append(notices, sent...); return nil },
	}
	call := func(member, name, args string) (any, error) {
		return tools.Call(t.Context(), Identity{Team: "custom", Member: member}, name, json.RawMessage(args))
	}
	pending, err := call("release-manager", "pending_release", `{}`)
	if err != nil || len(pending.([]taskSummary)) != 1 {
		t.Fatal(pending, err)
	}
	view, err := call("release-manager", "task_workflow", `{"id":"T-1"}`)
	if err != nil || !slices.Contains(view.(team.WorkflowView).ValidMoves, "Published") {
		t.Fatal(view, err)
	}
	if _, err := call("lead", "move_task", `{"id":"T-1","state":"Published","version":"v1.0"}`); err == nil {
		t.Fatal("lead bypassed release authority")
	}
	if _, err := call("release-manager", "move_task", `{"id":"T-1","state":"Published"}`); err == nil {
		t.Fatal("missing version accepted")
	}
	if _, err := call("release-manager", "move_task", `{"id":"T-1","state":"Published","version":"v1.0"}`); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	task := loaded.Board.Tasks[0]
	if task.ReleaseVersion != "v1.0" || !task.CompletedAt.Equal(completed) || task.ShippedAt.IsZero() {
		t.Fatal("release metadata lost", task)
	}
	pending, err = call("release-manager", "pending_release", `{}`)
	if err != nil || len(pending.([]taskSummary)) != 0 {
		t.Fatal("shipped task still pending", pending, err)
	}
	if _, err := call("release-manager", "move_task", `{"id":"T-1","state":"Published","version":"v1.0"}`); err == nil {
		t.Fatal("shipped terminal move accepted")
	}
	if !slices.ContainsFunc(notices, func(notice team.TaskNotice) bool {
		return notice.To == "lead" && strings.Contains(notice.Text, "v1.0")
	}) {
		t.Fatal("shipment notice missing lead/version attribution", notices)
	}
}
