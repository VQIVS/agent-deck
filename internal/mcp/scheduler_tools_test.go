package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

type fakeSchedulerActions struct {
	actions []SchedulerAction
	err     error
}

func (f *fakeSchedulerActions) ApplySchedulerAction(_ context.Context, action *SchedulerAction) (any, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.actions = append(f.actions, *action)
	return "queued", nil
}

func schedulerToolFixture() (*SchedulerTools, *SchedulerSnapshot, *fakeSchedulerActions) {
	snapshot := &SchedulerSnapshot{
		Team: "test", Revision: "revision-1",
		Members: []Member{{ID: "master-1", Name: "alice", Role: "master"}, {ID: "worker-1", Name: "bob", Role: "worker"}},
		Input: &team.SchedulingInput{
			Board: &team.TaskBoard{Version: 1, Tasks: []team.Task{{
				ID: "task", Title: "task", State: "pending", Paths: team.Leases{"internal/example/**"},
				Estimate: team.TaskEstimate{Parallel: true},
			}}},
			Policy: team.SchedulingPolicy{MaxWorkers: 3, Cooldown: time.Minute}, Now: time.Unix(10000, 0),
		},
	}
	actions := &fakeSchedulerActions{}
	tools := &SchedulerTools{
		Snapshot: func(context.Context) (*SchedulerSnapshot, error) { return snapshot, nil }, Actions: actions,
	}
	return tools, snapshot, actions
}

func TestSchedulerToolsMasterOnlyAndApprovalNotCallerControlled(t *testing.T) {
	tools, snapshot, actions := schedulerToolFixture()
	master := Identity{"test", "alice"}
	for _, identity := range []Identity{{"test", "bob"}, {"other", "alice"}, {"test", "unknown"}} {
		if _, err := tools.Call(t.Context(), identity, "propose_workers", nil); err == nil {
			t.Fatal("unauthorized scheduler call", identity)
		}
	}
	if _, err := tools.Call(t.Context(), master, "propose_workers", json.RawMessage(`{"approved":true}`)); err == nil {
		t.Fatal("caller approval accepted")
	}
	if _, err := tools.Call(t.Context(), master, "propose_workers", nil); err != nil {
		t.Fatal(err)
	}
	if len(actions.actions) != 1 || !actions.actions[0].Plan.Proposals[0].RequiresApproval ||
		actions.actions[0].Revision != "revision-1" || actions.actions[0].Master != "alice" {
		t.Fatal("lost default approval or revision", actions.actions)
	}
	snapshot.Members[0].Role = "worker"
	if _, err := tools.Call(t.Context(), master, "propose_workers", nil); err == nil {
		t.Fatal("demoted master retained access")
	}
	if len(actions.actions) != 1 {
		t.Fatal("rejected calls reached action transport")
	}
}

func TestSchedulerToolsAssignmentAndRetirement(t *testing.T) {
	tools, snapshot, actions := schedulerToolFixture()
	snapshot.Input.Workers = []team.SchedulerWorker{{Name: "bob", State: "idle"}}
	master := Identity{"test", "alice"}
	arguments := json.RawMessage(`{"task":"task","worker":"bob"}`)
	if _, err := tools.Call(t.Context(), master, "assign_task", arguments); err != nil {
		t.Fatal(err)
	}
	if snapshot.Input.Board.Tasks[0].State != "pending" || actions.actions[0].Worker != "bob" {
		t.Fatal("adapter mutated board or lost assignment")
	}
	for _, raw := range []string{`{"task":"missing","worker":"bob"}`, `{"task":"task","worker":"unknown"}`} {
		if _, err := tools.Call(t.Context(), master, "assign_task", json.RawMessage(raw)); err == nil {
			t.Fatal("invalid assignment accepted")
		}
	}
	snapshot.Input.Board.Tasks = nil
	snapshot.Input.Workers[0].IdleSince = snapshot.Input.Now.Add(-2 * time.Minute)
	if _, err := tools.Call(t.Context(), master, "retire_worker", json.RawMessage(`{"worker":"bob"}`)); err != nil {
		t.Fatal(err)
	}
	snapshot.Input.Workers[0].State = "busy"
	if _, err := tools.Call(t.Context(), master, "retire_worker", json.RawMessage(`{"worker":"bob"}`)); err == nil {
		t.Fatal("busy retirement accepted")
	}
}

func TestSchedulerToolsReadOnlyOffAndStaleRevisionTransport(t *testing.T) {
	tools, snapshot, actions := schedulerToolFixture()
	master := Identity{"test", "alice"}
	snapshot.Input.ReadOnly = true
	if _, err := tools.Call(t.Context(), master, "propose_workers", nil); err == nil {
		t.Fatal("read-only mutation accepted")
	}
	snapshot.Input.ReadOnly = false
	snapshot.Input.Policy.Approval = "off"
	if _, err := tools.Call(t.Context(), master, "propose_workers", nil); err == nil {
		t.Fatal("off scaling accepted")
	}
	snapshot.Input.Policy.Approval = "auto"
	actions.err = errors.New("revision changed")
	if _, err := tools.Call(t.Context(), master, "propose_workers", nil); !errors.Is(err, actions.err) {
		t.Fatal("action transport failure lost", err)
	}
	snapshot.Revision = ""
	if _, err := tools.Call(t.Context(), master, "propose_workers", nil); err == nil {
		t.Fatal("unversioned snapshot accepted")
	}
}
