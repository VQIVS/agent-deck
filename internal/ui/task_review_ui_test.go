package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestTaskReviewFormRequiresDecisionAndComment(t *testing.T) {
	task := &team.Task{ID: "T-1", State: "review"}
	form := NewKanbanForm("review", task, 7)
	if KanbanFormKey(form, tea.KeyMsg{Type: tea.KeyCtrlS}, false) != nil || form.Error == "" {
		t.Fatal("empty review submitted")
	}
	form.Fields[0].Value = "accept"
	form.Fields[1].Value = "Verified the required checks"
	edit := KanbanFormKey(form, tea.KeyMsg{Type: tea.KeyCtrlS}, false)
	if edit == nil || edit.Intent.TaskID != "T-1" || edit.Intent.State != "accept" || edit.Revision != 7 {
		t.Fatal("review identity lost")
	}
	if KanbanFormKey(form, tea.KeyMsg{Type: tea.KeyCtrlS}, true) != nil {
		t.Fatal("read-only review submitted")
	}
	form.Fields[0].Value = "maybe"
	if _, err := form.Build(); err == nil {
		t.Fatal("invalid review decision accepted")
	}
}

func TestTaskReviewDetailShowsMergedDoDSeparately(t *testing.T) {
	tasks, now := kanbanFixture()
	current := &team.Team{DefinitionOfDone: team.DefinitionOfDone{
		{ID: "security", Text: "Review permission boundaries", Required: true},
	}}
	screen := &KanbanScreen{Selected: "W3", Detail: true}
	view, _ := RenderKanbanReview(tasks, screen, &KanbanOptions{Width: 160, Height: 80, Now: now}, current)
	plain := ansi.Strip(view)
	if !strings.Contains(plain, "Acceptance criteria (claimed progress)") ||
		!strings.Contains(plain, "Merged definition of done") || !strings.Contains(plain, "security [required]") {
		t.Fatalf("merged requirements absent: %s", plain)
	}
}
