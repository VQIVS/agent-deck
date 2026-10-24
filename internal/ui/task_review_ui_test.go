package ui

import (
	"strings"
	"testing"
	"time"

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

func TestTaskReviewChangePersistsBothDecisionsAndNotices(t *testing.T) {
	for _, decision := range []string{"accept", "changes"} {
		t.Run(decision, func(t *testing.T) {
			e, state, now := kanbanExecutorFixture(t)
			task := &state.Board.Tasks[0]
			task.State, task.Reviewer = "review", "Sol"
			if err := e.Store.Save(t.Context(), &state, state.Revision); err != nil {
				t.Fatal(err)
			}
			board, notices, err := ApplyTaskReview(t.Context(), e.Store, e.Team,
				"A", decision, "Reviewed current work", now.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			want := "done"
			if decision == "changes" {
				want = "in_progress"
			}
			if board.Tasks[0].State != want || len(notices) == 0 {
				t.Fatalf("review not persisted/notified: %+v %+v", board, notices)
			}
			stored, err := e.Store.Load(t.Context())
			if err != nil || stored.Board.Tasks[0].State != want {
				t.Fatal("review missing from storage")
			}
		})
	}
}
