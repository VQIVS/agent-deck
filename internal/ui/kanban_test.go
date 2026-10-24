package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/team"
)

func kanbanFixture() ([]team.Task, time.Time) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	tasks := make([]team.Task, 0, 7)
	titles := []string{
		"Plan upgrade", "Add task schema", "Build Kanban", "Review scheduler",
		"Verify tests", "Resolve API", "Old experiment",
	}
	for i, state := range KanbanStates() {
		id := fmt.Sprintf("W%d", i+1)
		tasks = append(tasks, team.Task{
			ID: id, Title: titles[i],
			State: state, Assignee: []string{"", "lease", "health", "Sol", "mcp", "health", ""}[i],
			Priority: []string{"low", "high", "urgent", "normal", "high", "normal", "low"}[i],
			Labels:   []string{"workflow", "v0.2"}, CreatedAt: now.Add(-time.Duration(i+1) * time.Hour),
			Criteria: []team.TaskCriterion{
				{ID: "tests", Text: "Package tests pass", Checked: i > 2}, {ID: "review", Text: "Changes reviewed"},
			},
			Paths: team.Leases{"internal/ui/**"}, Description: "Render the workflow clearly and preserve task identity.",
			Reviewer: "Sol", Branch: "feat/workflow", Commits: []string{"abc123 add task view"},
			History:  []team.TaskChange{{Actor: "Sol", At: now.Add(-time.Hour), Field: "state", From: "ready", To: state}},
			Comments: []team.TaskComment{{Actor: "health", At: now, Text: "Ready for integration review."}},
			Report:   &team.TaskReport{Text: "Focused tests pass", Tests: "go test ./internal/ui/..."},
		})
	}
	tasks[2].Dependencies = []string{"W2"}
	return tasks, now
}

func TestKanbanGolden(t *testing.T) {
	tasks, now := kanbanFixture()
	theme, _ := ResolveTheme("dark")
	for _, size := range [][2]int{{80, 24}, {160, 48}} {
		for _, detail := range []bool{false, true} {
			name := fmt.Sprintf("kanban_%dx%d", size[0], size[1])
			if detail {
				name += "_detail"
			}
			t.Run(name, func(t *testing.T) {
				screen := KanbanScreen{Selected: "W3", Detail: detail}
				view, targets := RenderKanban(tasks, &screen, &KanbanOptions{
					Width: size[0], Height: size[1], Now: now, Theme: theme,
				})
				if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
					t.Fatal("view exceeds terminal")
				}
				for _, target := range targets {
					if target.X < 0 || target.Y < 0 || target.X+target.W > size[0] || target.Y+target.H > size[1] {
						t.Fatalf("bad target: %+v", target)
					}
				}
				filename := filepath.Join("testdata", name+".golden")
				lines := strings.Split(ansi.Strip(view), "\n")
				for i := range lines {
					lines[i] = strings.TrimRight(lines[i], " \t")
				}
				actual := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
				if os.Getenv("UPDATE_GOLDEN") == "1" {
					if err := os.WriteFile(filename, []byte(actual), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				want, err := os.ReadFile(filename)
				if err != nil {
					t.Fatal(err)
				}
				if actual != string(want) {
					t.Fatalf("golden mismatch %s:\n%s", filename, actual)
				}
			})
		}
	}
}

func TestKanbanNavigationIdentityAndIntents(t *testing.T) {
	tasks, _ := kanbanFixture()
	s := KanbanScreen{Selected: "W3"}
	KanbanKey(tasks, &s, key("m"), false)
	intent := KanbanKey(tasks, &s, key("4"), false)
	if intent == nil || intent.TaskID != "W3" || intent.State != "review" || tasks[2].State != "in_progress" {
		t.Fatal("move mutated or lost identity")
	}
	KanbanKey(tasks, &s, key("m"), true)
	if s.Move || KanbanKey(tasks, &s, key("e"), true) != nil {
		t.Fatal("read-only mutation emitted")
	}
	intent = KanbanKey(tasks, &s, key("w"), true)
	if intent == nil || intent.Assignee != "health" {
		t.Fatal("worker link unavailable read-only")
	}
	KanbanKey(tasks, &s, tea.KeyMsg{Type: tea.KeyRight}, false)
	if s.Selected != "W4" {
		t.Fatal("column navigation failed")
	}
	s.Filter = KanbanFilter{Assignee: "health", Label: "workflow"}
	s.Selected = "W3"
	s.reconcile(tasks)
	stale := KanbanTarget{TaskID: "W3", Action: "detail", State: "in_progress"}
	tasks[2].State = "review"
	KanbanClick(tasks, &s, &stale, false)
	if s.Detail {
		t.Fatal("stale click accepted")
	}
	KanbanClick(tasks, &s, &KanbanTarget{TaskID: "W3", Action: "detail", State: "review"}, false)
	if !s.Detail {
		t.Fatal("current detail click ignored")
	}
}

func TestKanbanDetailAndFiltering(t *testing.T) {
	tasks, now := kanbanFixture()
	s := KanbanScreen{Selected: "W3", Detail: true}
	view, _ := RenderKanban(tasks, &s, &KanbanOptions{Width: 160, Height: 80, Now: now})
	for _, text := range []string{
		"Acceptance criteria (claimed progress)", "abc123", "Worker pane: health", "Focused tests pass",
		"History", "Comments", "Ready for integration review.",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("missing detail %q", text)
		}
	}
	if len(kanbanTasks(tasks, KanbanFilter{Assignee: "health", State: "blocked"}, "", false)) != 1 {
		t.Fatal("conjunctive filters failed")
	}
	if kanbanAge(time.Time{}, now) != "age unknown" {
		t.Fatal("zero timestamp age guessed")
	}
	s = KanbanScreen{Column: "ready", Filter: KanbanFilter{Query: "no matching task"}}
	view, targets := RenderKanban(tasks, &s, &KanbanOptions{Width: 80, Height: 24, Now: now})
	if len(targets) != 0 || s.Selected != "" || view == "" {
		t.Fatal("empty filter projection broken")
	}
}

func TestKanbanMoveNeverRetargetsAndFiltersCycle(t *testing.T) {
	tasks, _ := kanbanFixture()
	s := KanbanScreen{Selected: "W3"}
	KanbanKey(tasks, &s, key("m"), false)
	s.Selected = "W4"
	intent := KanbanKey(tasks, &s, key("6"), false)
	if intent == nil || intent.TaskID != "W3" {
		t.Fatal("move retargeted after selection changed")
	}
	s.Selected = "W3"
	KanbanKey(tasks, &s, key("m"), false)
	tasks = append(tasks[:2], tasks[3:]...)
	if KanbanKey(tasks, &s, key("4"), false) != nil {
		t.Fatal("removed task emitted move")
	}
	KanbanKey(tasks, &s, key("f"), false)
	KanbanKey(tasks, &s, key("t"), false)
	KanbanKey(tasks, &s, key("z"), false)
	if s.Filter.Assignee == "" || s.Filter.Label == "" || s.Filter.State == "" {
		t.Fatal("filter cycling failed")
	}
	KanbanKey(tasks, &s, key("0"), false)
	if s.Filter != (KanbanFilter{}) {
		t.Fatal("clear filters failed")
	}
}

func TestKanbanSmallSizesSwimlanesAndSanitization(t *testing.T) {
	tasks, now := kanbanFixture()
	tasks[2].Title = "\x1b[31mInjected\x1b[0m\nnew line"
	for _, size := range [][2]int{{1, 1}, {20, 8}, {80, 24}} {
		s := KanbanScreen{Selected: "W3", Swimlanes: true}
		view, targets := RenderKanban(tasks, &s, &KanbanOptions{Width: size[0], Height: size[1], Now: now})
		if strings.Contains(view, "\x1b") || lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
			t.Fatalf("unbounded or unsafe render %dx%d: %q", size[0], size[1], view)
		}
		for _, target := range targets {
			if target.Y+target.H > size[1] {
				t.Fatal("offscreen mouse target")
			}
		}
	}
}

func TestKanbanClickTimingAndMoveButtons(t *testing.T) {
	tasks, now := kanbanFixture()
	s := KanbanScreen{Selected: "W3"}
	target := KanbanTarget{TaskID: "W3", Action: "select", State: "in_progress"}
	KanbanClickAt(tasks, &s, &target, false, now)
	if s.Detail {
		t.Fatal("single click opened detail")
	}
	KanbanClickAt(tasks, &s, &target, false, now.Add(200*time.Millisecond))
	if !s.Detail {
		t.Fatal("double click failed to open detail")
	}
	intent := KanbanClick(tasks, &s, &KanbanTarget{TaskID: "W3", Action: "next", State: "in_progress"}, false)
	if intent == nil || intent.TaskID != "W3" || intent.State != "review" {
		t.Fatal("move button emitted wrong target")
	}
	identity := "health"
	if MemberColor(identity) != MemberColor(string([]byte(identity))) {
		t.Fatal("member color unstable")
	}
}

func TestKanbanAssignedReadyCardSaysWaiting(t *testing.T) {
	tasks, now := kanbanFixture()
	screen := KanbanScreen{Selected: "W2"}
	view, _ := RenderKanban(tasks, &screen, &KanbanOptions{Width: 80, Height: 24, Now: now})
	if !strings.Contains(ansi.Strip(view), "waiting for lease") {
		t.Fatal("ready assignment looks active")
	}
	tasks[1].State = "in_progress"
	view, _ = RenderKanban(tasks, &screen, &KanbanOptions{Width: 80, Height: 24, Now: now})
	if strings.Contains(ansi.Strip(view), "waiting for lease") {
		t.Fatal("active assignment still marked waiting")
	}
}

func TestKanbanAdaptiveColumns(t *testing.T) {
	tasks, _ := kanbanFixture()
	s := KanbanScreen{Column: "in_progress"}
	first, widths := kanbanColumnWidths(tasks, &s, 160)
	if first != 0 || len(widths) != 7 {
		t.Fatalf("wide board hides states: %d %v", first, widths)
	}
	_, widths = kanbanColumnWidths(tasks, &s, 80)
	if len(widths) != 2 || widths[0] < 30 || widths[1] < 30 {
		t.Fatalf("narrow board squeezes cards: %v", widths)
	}
	tasks = tasks[:1]
	_, widths = kanbanColumnWidths(tasks, &s, 110)
	if len(widths) != 7 || widths[1] > 12 {
		t.Fatalf("empty states not collapsed: %v", widths)
	}
}
