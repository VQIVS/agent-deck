package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
)

func kanbanExecutorFixture(t *testing.T) (*KanbanExecutor, team.BoardState, time.Time) {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	tasks := make([]team.Task, 0, 2)
	for _, id := range []string{"A", "B"} {
		tasks = append(tasks, team.Task{
			ID: id, Title: id, State: "ready", Assignee: "worker",
			Paths: team.Leases{id + "/**"}, Criteria: []team.TaskCriterion{{ID: "test", Text: "Tests pass"}},
			CreatedAt: now, UpdatedAt: now,
		})
	}
	e := &KanbanExecutor{
		Store: team.BoardStore{Root: t.TempDir(), Team: "pool"},
		Team: &team.Team{Name: "pool", Members: []team.Member{
			{Name: "Sol", Role: "master"}, {Name: "worker", Role: "worker"},
		}},
	}
	state := team.BoardState{
		Version: 2, Board: team.TaskBoard{Version: 2, Tasks: tasks}, Policy: team.SchedulingPolicy{MaxWorkers: 2},
	}
	if err := e.Store.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	return e, state, now
}

func TestKanbanExecutorNoticesAndOneActiveFailure(t *testing.T) {
	e, state, now := kanbanExecutorFixture(t)
	var delivered []team.TaskNotice
	e.Notify = func(_ context.Context, notices []team.TaskNotice) error {
		delivered = notices
		return errors.New("delivery failed")
	}
	edit := &KanbanEdit{Intent: KanbanIntent{Action: "move", TaskID: "A", State: "in_progress"}, Revision: state.Revision}
	result, err := e.Apply(t.Context(), edit, now.Add(time.Minute))
	if err != nil || result.NotifyErr == nil || len(delivered) == 0 {
		t.Fatalf("save/notices: %+v %v", result, err)
	}
	current, err := e.Store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	edit = &KanbanEdit{Intent: KanbanIntent{Action: "move", TaskID: "B", State: "in_progress"}, Revision: current.Revision}
	_, err = e.Apply(t.Context(), edit, now.Add(2*time.Minute))
	if err == nil || !strings.Contains(err.Error(), "active") {
		t.Fatalf("one-active failure not visible: %v", err)
	}
	current, err = e.Store.Load(t.Context())
	if err != nil || current.Board.Tasks[1].State != "ready" {
		t.Fatal("failed mutation persisted")
	}
}

func TestKanbanExecutorChangeRetriesUnrelatedCollision(t *testing.T) {
	e, state, now := kanbanExecutorFixture(t)
	edit := &KanbanEdit{
		Intent: KanbanIntent{Action: "comment", TaskID: "A"}, Comment: "UI comment", Revision: state.Revision,
	}
	baseline, err := kanbanEditBaseline(&state.Board, edit)
	if err != nil {
		t.Fatal(err)
	}
	change := e.change(edit, baseline)
	attempts := 0
	board, _, err := team.ChangeTasks(t.Context(), e.Store, e.Team, team.TaskActor{User: true}, now.Add(time.Minute),
		func(board *team.TaskBoard, m team.TaskMutation) error {
			attempts++
			if attempts == 1 {
				competing, loadErr := e.Store.Load(t.Context())
				if loadErr != nil {
					return loadErr
				}
				if err := competing.Board.CommentTask("B", "parallel comment", m); err != nil {
					return err
				}
				if err := e.Store.Save(t.Context(), &competing, competing.Revision); err != nil {
					return err
				}
			}
			return change(board, m)
		})
	if err != nil || attempts != 2 || len(board.Tasks[0].Comments) != 1 || len(board.Tasks[1].Comments) != 1 {
		t.Fatalf("collision lost edits: attempts=%d board=%+v err=%v", attempts, board, err)
	}
}

func TestKanbanExecutorStaleAndReadOnly(t *testing.T) {
	e, state, now := kanbanExecutorFixture(t)
	edit := &KanbanEdit{
		Intent: KanbanIntent{Action: "comment", TaskID: "A"}, Comment: "comment", Revision: state.Revision - 1,
	}
	edit.Original = "stale task digest"
	if _, err := e.Apply(t.Context(), edit, now); err == nil {
		t.Fatal("stale form saved")
	}
	e.ReadOnly = true
	edit.Revision = state.Revision
	if _, err := e.Apply(t.Context(), edit, now); err == nil {
		t.Fatal("read-only saved")
	}
}
