package team

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestWorkflowSingleActiveAndDependencyEdit(t *testing.T) {
	board, m := workflowFixture(t)
	if err := board.SetAssignee("task", "worker", m); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"ready", "in_progress"} {
		if err := board.MoveTask("task", state, m); err != nil {
			t.Fatal(err)
		}
	}
	second := &Task{
		ID: "second", Title: "second", Paths: Leases{"second/**"},
		Criteria: []TaskCriterion{{ID: "check", Text: "check"}},
	}
	if err := board.CreateTask(second, m); err != nil {
		t.Fatal(err)
	}
	if err := board.SetAssignee("second", "worker", m); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask("second", "ready", m); err != nil {
		t.Fatal(err)
	}
	if err := board.MoveTask("second", "in_progress", m); err == nil {
		t.Fatal("two active tasks accepted")
	}
	edited := cloneTask(&board.Tasks[0])
	edited.Dependencies = []string{"second"}
	if err := board.UpdateTask("task", &edited, m); err == nil {
		t.Fatal("unfinished dependency added to active task")
	}
}

func TestWorkflowAggregateBudgetAndUnlockedLoad(t *testing.T) {
	board := &TaskBoard{Version: 2}
	for i := 0; i < 8; i++ {
		task := Task{ID: fmt.Sprint(i), Title: "task", State: "backlog"}
		for j := 0; j < 60; j++ {
			task.Comments = append(task.Comments, TaskComment{
				ID: fmt.Sprint(j), Actor: "user", At: time.Now(), Text: strings.Repeat("x", 16000),
			})
		}
		board.Tasks = append(board.Tasks, task)
	}
	if err := board.Validate(); err == nil {
		t.Fatal("aggregate board over budget accepted")
	}
	store := BoardStore{Root: t.TempDir(), Team: "migration"}
	legacy := BoardState{Version: 2, Board: TaskBoard{Version: 2}, Policy: SchedulingPolicy{}}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := store.Exclusive(ctx, func() error {
		if err := store.write(ctx, "board.json", &legacy); err != nil {
			return err
		}
		state, err := store.LoadUnlocked(ctx)
		if err == nil && state.Version != 2 {
			t.Fatal("unlocked load changed version")
		}
		return err
	}); err != nil {
		t.Fatal("nested lock in unlocked load", err)
	}
}

func TestReviewReassignmentAndCompactCommentHistory(t *testing.T) {
	board, m := workflowFixture(t)
	if err := board.SetAssignee("task", "worker", m); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"ready", "in_progress", "review"} {
		if err := board.MoveTask("task", state, m); err != nil {
			t.Fatal(err)
		}
	}
	if err := board.SetAssignee("task", "other", m); err != nil {
		t.Fatal(err)
	}
	if board.Tasks[0].State != "ready" || board.Tasks[0].Review != nil {
		t.Fatal("review reassignment retained review")
	}
	if err := board.CommentTask("task", "large comment body", m); err != nil {
		t.Fatal(err)
	}
	task := &board.Tasks[0]
	event := task.History[len(task.History)-1]
	if event.To != task.Comments[0].ID || event.To == task.Comments[0].Text {
		t.Fatal("history duplicates comment text")
	}
	for i := len(task.Comments); i < 500; i++ {
		task.Comments = append(task.Comments, TaskComment{ID: "bounded", Actor: "user", At: m.Now, Text: "x"})
	}
	if err := board.CommentTask("task", "overflow", m); err == nil {
		t.Fatal("unbounded comments")
	}
}

func TestTaskActorResolved(t *testing.T) {
	tm := Team{Members: []Member{{Name: "worker", Role: "worker", Session: "ses_worker"}}}
	actor, err := tm.ResolveTaskActor("worker")
	if err != nil || actor.Role != "worker" {
		t.Fatal(actor, err)
	}
	if _, err := tm.ResolveTaskActor("missing"); err == nil {
		t.Fatal("unknown actor authorized")
	}
	tm.Members[0].Muted = true
	if _, err := tm.ResolveTaskActor("worker"); err == nil {
		t.Fatal("muted actor authorized")
	}
}
