package ui

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/server"
	"github.com/vqivs/agent-deck/internal/team"
)

type kanbanBoardNotifier struct {
	fakeTeamChannel
	Notices []team.TaskNotice
}

func (n *kanbanBoardNotifier) Notify(_ context.Context, notices []team.TaskNotice) error {
	n.Notices = append(n.Notices, notices...)
	return nil
}

func kanbanBoardModel(t *testing.T) (*Model, team.BoardStore, *kanbanBoardNotifier) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	m := requestModel("http://unused.invalid", false)
	m.w, m.h = 160, 48
	m.teams = &teamUI{Manager: &team.Manager{Team: team.Team{
		Name: "pool", Server: m.deck.Server,
		Members: []team.Member{
			{Name: "Sol", Role: "master"},
			{Name: "Worker", Role: "worker", Dir: "/tmp/team", Session: "session-a"},
		},
	}}}
	root, err := server.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	store := team.BoardStore{Root: filepath.Join(root, "boards"), Team: "pool"}
	state := team.BoardState{
		Version: 2, Board: team.TaskBoard{Version: 2},
		Policy: team.SchedulingPolicy{MaxWorkers: 2},
	}
	if err := store.Save(t.Context(), &state, 0); err != nil {
		t.Fatal(err)
	}
	m.board.State = state
	notifier := &kanbanBoardNotifier{}
	m.EnableChannel(notifier)
	return m, store, notifier
}

func setKanbanBoardField(t *testing.T, form *KanbanForm, label, value string) {
	t.Helper()
	for i := range form.Fields {
		if form.Fields[i].Label == label {
			form.Fields[i].Value = value
			return
		}
	}
	t.Fatalf("missing task field %q", label)
}

func saveKanbanBoardCommand(t *testing.T, m *Model, cmd tea.Cmd) kanbanResult {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected task save command")
	}
	result, ok := cmd().(kanbanResult)
	if !ok {
		t.Fatal("unexpected task save result")
	}
	m.applyKanbanResult(&result)
	return result
}

func TestKanbanBoardWrapperOpensAndMouseTargetsCurrentWorker(t *testing.T) {
	m, _, _ := kanbanBoardModel(t)
	tasks, _ := kanbanFixture()
	tasks[2].Assignee, tasks[2].Worker = "Worker", "Worker"
	m.board.State.Board.Tasks = tasks
	m.board.Kanban.Selected = "W3"
	m.Update(key("b"))
	view := ansi.Strip(m.View())
	if !m.board.Open || !strings.Contains(view, "Build Kanban") || !strings.Contains(view, "┏") {
		t.Fatalf("board key did not show cards: %s", view)
	}
	var row zone
	found := false
	for i := range m.board.Targets {
		target := &m.board.Targets[i]
		if target.TaskID == "W3" && target.Action == "select" {
			row, found = hit(m.zones, target.X+2, target.Y+1)
			break
		}
	}
	if !found {
		t.Fatal("card geometry does not hit wrapper zone")
	}
	m.board.Kanban.Selected = "W2"
	m.kanbanBoardClick(row, true)
	if m.board.Kanban.Selected != "W3" || m.board.Kanban.Detail {
		t.Fatal("single card click did not select only")
	}
	m.board.Kanban.lastClickAt = time.Now()
	m.kanbanBoardClick(row, true)
	if !m.board.Kanban.Detail {
		t.Fatal("second card click did not open detail")
	}
	m.boardKey(key("w"))
	if m.board.Open || !m.zoom || m.panes[m.focus].Agent.Session != "session-a" {
		t.Fatal("worker link did not resolve current session")
	}
}

func TestKanbanBoardCreateFormPersistsAndNotifiesOnStart(t *testing.T) {
	m, store, notifier := kanbanBoardModel(t)
	m.board.Open = true
	m.boardKey(key("n"))
	if m.board.Form == nil {
		t.Fatal("create did not open form")
	}
	for label, value := range map[string]string{
		"ID": "UI-1", "Title": "Create from board", "Description": "A real UI form submission",
		"Criteria (id: text per line)": "tests: Package tests pass", "Paths": "internal/ui/**",
		"Priority": "high", "Assignee": "Worker", "Reviewer": "Sol", "Estimate": "10m",
	} {
		setKanbanBoardField(t, m.board.Form, label, value)
	}
	result := saveKanbanBoardCommand(t, m, m.boardKey(tea.KeyMsg{Type: tea.KeyCtrlS}))
	if result.Err != nil {
		t.Fatal(result.Err)
	}
	state, err := store.Load(t.Context())
	if err != nil || len(state.Board.Tasks) != 1 || state.Board.Tasks[0].History[0].Actor != "user" {
		t.Fatalf("form did not persist attributed task: %+v %v", state, err)
	}
	m.board.State = state
	for _, target := range []string{"ready", "in_progress"} {
		cmd := m.kanbanIntent(&KanbanIntent{Action: "move", TaskID: "UI-1", State: target})
		if result = saveKanbanBoardCommand(t, m, cmd); result.Err != nil {
			t.Fatal(result.Err)
		}
		state, err = store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		m.board.State = state
	}
	if !slices.ContainsFunc(notifier.Notices, func(n team.TaskNotice) bool {
		return n.To == "Worker" && strings.Contains(n.Text, "Create from board")
	}) {
		t.Fatalf("runtime did not receive task brief: %+v", notifier.Notices)
	}
}

func TestKanbanBoardOneActiveErrorAppearsWithoutSaving(t *testing.T) {
	m, store, _ := kanbanBoardModel(t)
	now := time.Now().UTC()
	board, _, err := team.ChangeTasks(t.Context(), store, &m.teams.Manager.Team, team.TaskActor{User: true}, now,
		func(board *team.TaskBoard, mutation team.TaskMutation) error {
			for _, id := range []string{"A", "B"} {
				task := team.Task{
					ID: id, Title: id, Assignee: "Worker", Worker: "Worker",
					Paths: team.Leases{id + "/**"}, Criteria: []team.TaskCriterion{{ID: "test", Text: "Pass"}},
				}
				if err := board.CreateTask(&task, mutation); err != nil {
					return err
				}
				if err := board.MoveTask(id, "ready", mutation); err != nil {
					return err
				}
			}
			return board.MoveTask("A", "in_progress", mutation)
		})
	if err != nil || len(board.Tasks) != 2 {
		t.Fatal(err)
	}
	m.board.State, err = store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	m.board.Open = true
	result := saveKanbanBoardCommand(t, m,
		m.kanbanIntent(&KanbanIntent{Action: "move", TaskID: "B", State: "in_progress"}))
	if result.Err == nil || !strings.Contains(m.note, "active") || !strings.Contains(m.boardView(), m.note) {
		t.Fatalf("validation failure not visible on board: %q %v", m.note, result.Err)
	}
	state, err := store.Load(t.Context())
	if err != nil || state.Board.Tasks[1].State != "ready" {
		t.Fatal("failed start changed stored task")
	}
}
