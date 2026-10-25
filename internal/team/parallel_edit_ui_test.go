package team_test

import (
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/team"
	"github.com/vqivs/agent-deck/internal/ui"
)

func TestParallelSurvivesKanbanFormSaveReload(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		t.Run(map[bool]string{false: "serial", true: "parallel"}[parallel], func(t *testing.T) {
			configured := &team.Team{Name: "parallel-test"}
			store := team.BoardStore{Root: t.TempDir(), Team: configured.Name}
			executor := &ui.KanbanExecutor{Store: store, Team: configured}
			original := team.Task{
				ID: "T-1", Title: "Original", State: "backlog", Paths: []string{"src/**"},
				Criteria: []team.TaskCriterion{{ID: "c1", Text: "Pass"}},
				Estimate: team.TaskEstimate{Parallel: parallel, Duration: time.Minute, Tokens: 100},
			}
			if _, err := executor.Apply(t.Context(), &ui.KanbanEdit{
				Intent: ui.KanbanIntent{Action: "create", TaskID: original.ID}, Task: &original,
			}, time.Now()); err != nil {
				t.Fatal(err)
			}
			loaded, err := store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			form := ui.NewKanbanForm("edit", &loaded.Board.Tasks[0], loaded.Revision)
			for i := range form.Fields {
				switch form.Fields[i].Label {
				case "Title":
					form.Fields[i].Value = "Renamed"
				case "Estimate":
					form.Fields[i].Value = "20m"
				}
			}
			edit, err := form.Build()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := executor.Apply(t.Context(), edit, time.Now()); err != nil {
				t.Fatal(err)
			}
			loaded, err = store.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			got := loaded.Board.Tasks[0]
			if got.Title != "Renamed" || got.Estimate.Duration != 20*time.Minute ||
				got.Estimate.Parallel != parallel || got.Estimate.Tokens != 100 {
				t.Fatalf("form edit lost task estimate: %+v", got)
			}
		})
	}
}
