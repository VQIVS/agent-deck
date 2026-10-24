package team

import (
	"context"
	"strings"
	"testing"
	"time"
)

func workflowTeam() *Team {
	return &Team{Name: "pool", Members: []Member{
		{Name: "lead", Role: "master"}, {Name: "ana", Role: "worker"}, {Name: "rev", Role: "worker"},
	}}
}

func noticeFor(notices []TaskNotice, to, contains string) bool {
	for _, n := range notices {
		if n.To == to && strings.Contains(n.Text, contains) {
			return true
		}
	}
	return false
}

func TestTaskActorForUsesRoster(t *testing.T) {
	team := workflowTeam()
	actor, err := TaskActorFor(team, "lead")
	if err != nil || actor.Role != "master" || actor.Member != "lead" {
		t.Fatalf("actor=%+v err=%v", actor, err)
	}
	if _, err := TaskActorFor(team, "intruder"); err == nil {
		t.Fatal("unknown member resolved")
	}
}

func TestChangeTasksFlowAndNotices(t *testing.T) {
	ctx := context.Background()
	team := workflowTeam()
	store := BoardStore{Root: t.TempDir(), Team: team.Name}
	lead, _ := TaskActorFor(team, "lead")
	ana, _ := TaskActorFor(team, "ana")
	rev, _ := TaskActorFor(team, "rev")
	now := time.Now()
	change := func(actor TaskActor, fn func(*TaskBoard, TaskMutation) error) []TaskNotice {
		t.Helper()
		_, notices, err := ChangeTasks(ctx, store, team, actor, now, fn)
		if err != nil {
			t.Fatal(err)
		}
		return notices
	}
	change(lead, func(b *TaskBoard, m TaskMutation) error {
		task := &Task{
			ID: "T-1", Title: "Add flag", Paths: Leases{"cmd/*"}, Reviewer: "rev",
			Criteria: []TaskCriterion{{ID: "c1", Text: "flag parsed"}},
		}
		if err := b.CreateTask(task, m); err != nil {
			return err
		}
		return b.MoveTask("T-1", "ready", m)
	})
	notices := change(lead, func(b *TaskBoard, m TaskMutation) error { return b.SetAssignee("T-1", "ana", m) })
	if !noticeFor(notices, "ana", "You are assigned task T-1") {
		t.Fatalf("assignee brief missing: %+v", notices)
	}
	notices = change(lead, func(b *TaskBoard, m TaskMutation) error { return b.MoveTask("T-1", "in_progress", m) })
	if noticeFor(notices, "ana", "You are assigned") {
		t.Fatal("brief sent twice")
	}
	notices = change(ana, func(b *TaskBoard, m TaskMutation) error { return b.MoveTask("T-1", "review", m) })
	if !noticeFor(notices, "rev", "ready for your review") || !noticeFor(notices, "lead", "moved to review") {
		t.Fatalf("review notices missing: %+v", notices)
	}
	notices = change(rev, func(b *TaskBoard, m TaskMutation) error {
		return b.ReviewTask("T-1", "changes", "handle empty flag", m)
	})
	if !noticeFor(notices, "ana", "requested changes") {
		t.Fatalf("changes notice missing: %+v", notices)
	}
	notices = change(ana, func(b *TaskBoard, m TaskMutation) error { return b.CommentTask("T-1", "fixed", m) })
	if noticeFor(notices, "ana", "") || !noticeFor(notices, "lead", "fixed") {
		t.Fatalf("comment notices wrong: %+v", notices)
	}
	change(ana, func(b *TaskBoard, m TaskMutation) error { return b.MoveTask("T-1", "review", m) })
	notices = change(rev, func(b *TaskBoard, m TaskMutation) error { return b.ReviewTask("T-1", "accept", "ok", m) })
	if !noticeFor(notices, "lead", "is now done") {
		t.Fatalf("done notice missing: %+v", notices)
	}
	state, err := store.Load(ctx)
	if err != nil || state.Board.Tasks[0].State != "done" || state.Revision != 8 {
		t.Fatalf("persisted state=%+v err=%v", state.Board.Tasks, err)
	}
}

func TestChangeTasksRejectsWorkerPlanning(t *testing.T) {
	team := workflowTeam()
	store := BoardStore{Root: t.TempDir(), Team: team.Name}
	ana, _ := TaskActorFor(team, "ana")
	_, _, err := ChangeTasks(context.Background(), store, team, ana, time.Now(), func(b *TaskBoard, m TaskMutation) error {
		return b.CreateTask(&Task{ID: "T-1", Title: "x", Paths: Leases{"a/*"}}, m)
	})
	if err == nil {
		t.Fatal("worker created a task")
	}
	if _, loadErr := store.Load(context.Background()); loadErr == nil {
		t.Fatal("failed change was persisted")
	}
}

func TestWorkflowInstructionsOnlyWithTools(t *testing.T) {
	team := workflowTeam()
	if workflowInstructions(team, &team.Members[0]) != "" {
		t.Fatal("manual added without tools")
	}
	team.LeasePool = true
	if !strings.Contains(workflowInstructions(team, &team.Members[0]), "you are the master") ||
		!strings.Contains(workflowInstructions(team, &team.Members[1]), "you are a worker") {
		t.Fatal("role manuals missing")
	}
}
