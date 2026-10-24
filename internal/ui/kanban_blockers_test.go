package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestKanbanFormBaselineCapturedBeforeConcurrentChange(t *testing.T) {
	e, state, now := kanbanExecutorFixture(t)
	form := NewKanbanForm("comment", &state.Board.Tasks[0], state.Revision)
	form.Fields[0].Value = "my comment"
	_, _, err := team.ChangeTasks(t.Context(), e.Store, e.Team, team.TaskActor{User: true}, now,
		func(board *team.TaskBoard, m team.TaskMutation) error {
			return board.CommentTask("A", "other comment", m)
		})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := form.Build()
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.Apply(t.Context(), edit, now.Add(time.Minute))
	if err == nil || !strings.Contains(err.Error(), "task changed") {
		t.Fatalf("stale form overwrote changed task: %v", err)
	}
}

func TestKanbanFormAllowsUnrelatedBoardChange(t *testing.T) {
	e, state, now := kanbanExecutorFixture(t)
	form := NewKanbanForm("comment", &state.Board.Tasks[0], state.Revision)
	form.Fields[0].Value = "my comment"
	_, _, err := team.ChangeTasks(t.Context(), e.Store, e.Team, team.TaskActor{User: true}, now,
		func(board *team.TaskBoard, m team.TaskMutation) error {
			return board.CommentTask("B", "other comment", m)
		})
	if err != nil {
		t.Fatal(err)
	}
	edit, err := form.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.Apply(t.Context(), edit, now.Add(time.Minute)); err != nil {
		t.Fatalf("unrelated change invalidated form: %v", err)
	}
}

func TestKanbanModesPrecedePoolAndReviewShortcuts(t *testing.T) {
	m := requestModel("http://unused.invalid", false)
	m.board.Actions = []team.ReconcileAction{{ID: "approval"}}
	m.board.Kanban.searching = true
	m.boardKey(key("p"))
	m.boardKey(key("v"))
	m.boardKey(key("y"))
	if m.board.Approvals || m.board.Confirm != nil || m.board.Form != nil || m.board.Kanban.Filter.Query != "pvy" {
		t.Fatal("search keys escaped into approval/review controls")
	}
	m.board.Kanban.searching = false
	m.boardKey(key("y"))
	if m.board.Confirm != nil {
		t.Fatal("Kanban y opened pool approval")
	}
}

func TestKanbanDropsOlderPeriodicBoardResult(t *testing.T) {
	m := requestModel("http://unused.invalid", false)
	m.board.State = team.BoardState{Revision: 10, Board: team.TaskBoard{Version: 2, Tasks: []team.Task{{ID: "new"}}}}
	m.updateHealth(boardResult{State: team.BoardState{Revision: 9}})
	if m.board.State.Revision != 10 || m.board.State.Board.Tasks[0].ID != "new" {
		t.Fatal("stale board result replaced saved state")
	}
}

func TestKanbanImmediateSecondSaveUsesExactRevision(t *testing.T) {
	m, _, _ := kanbanBoardModel(t)
	m.board.Open = true
	m.board.Form = NewKanbanForm("create", nil, m.board.State.Revision)
	setKanbanBoardField(t, m.board.Form, "ID", "T-ui")
	setKanbanBoardField(t, m.board.Form, "Title", "First title")
	edit, err := m.board.Form.Build()
	if err != nil {
		t.Fatal(err)
	}
	first := saveKanbanBoardCommand(t, m, m.saveKanbanEdit(edit))
	if first.Err != nil || first.Saved.State.Revision != 2 || m.board.State.Revision != 2 {
		t.Fatal("saved revision not adopted")
	}
	m.board.Form = NewKanbanForm("edit", &m.board.State.Board.Tasks[0], m.board.State.Revision)
	setKanbanBoardField(t, m.board.Form, "Title", "Second title")
	edit, err = m.board.Form.Build()
	if err != nil {
		t.Fatal(err)
	}
	second := saveKanbanBoardCommand(t, m, m.saveKanbanEdit(edit))
	if second.Err != nil || m.board.State.Revision != 3 || m.board.State.Board.Tasks[0].Title != "Second title" {
		t.Fatalf("immediate second edit failed: %+v %v", m.board.State, second.Err)
	}
	m.updateHealth(boardResult{State: first.Saved.State})
	if m.board.State.Revision != 3 {
		t.Fatal("older poll reverted second save")
	}
}

func TestKanbanDetailWheelScrollsWithoutChangingTask(t *testing.T) {
	m := requestModel("http://unused.invalid", false)
	tasks, _ := kanbanFixture()
	m.board.Open = true
	m.board.State.Board.Tasks = tasks
	m.board.Kanban = KanbanScreen{Selected: "W3", Detail: true}
	m.mouseWheel(tea.MouseMsg{Button: tea.MouseButtonWheelDown}, zone{}, false)
	if m.board.Kanban.Selected != "W3" || m.board.Kanban.DetailOffset != 1 {
		t.Fatal("wheel changed task instead of scrolling detail")
	}
}
