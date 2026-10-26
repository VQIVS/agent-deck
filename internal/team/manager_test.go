package team

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/server"
)

func TestOptionsAndTaskChangesIgnoreOldEstimationHistory(t *testing.T) {
	for _, data := range []string{
		`{"version":1,"samples":[{"task":"old-history-marker","estimate":{"duration":"1m"}}]}`,
		`invalid old-history-marker`,
	} {
		t.Run(data[:7], func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root, err := server.StateRoot()
			if err != nil {
				t.Fatal(err)
			}
			store := BoardStore{Root: filepath.Join(root, "boards"), Team: "history"}
			state := BoardState{Version: 2, Board: TaskBoard{Version: 2}}
			if err := store.Save(t.Context(), &state, 0); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(store.Root, store.Team, "estimation-history.json")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			configured := Team{Name: "history", Rules: "private rules"}
			options, err := Options(t.Context(), &configured, &Member{Role: "master"}, "plan")
			if err != nil || strings.Contains(options.System, "Team sizing history") ||
				strings.Contains(options.System, "old-history-marker") || !strings.Contains(options.System, "private rules") {
				t.Fatal("old history affected prompt", options.System, err)
			}
			estimate := TaskEstimate{Duration: time.Minute, Tokens: 100, Cost: 0.1}
			saved, notices, err := ChangeTasksState(t.Context(), store, &configured, TaskActor{User: true}, time.Now(),
				func(board *TaskBoard, mutation TaskMutation) error {
					return board.CreateTask(&Task{ID: "new", Title: "New work", Estimate: estimate}, mutation)
				})
			if err != nil || len(saved.Board.Tasks) != 1 || saved.Board.Tasks[0].Estimate != estimate {
				t.Fatal("old history affected task change or estimate", saved, err)
			}
			for _, notice := range notices {
				if notice.Task == "estimation-history" {
					t.Fatal("old history generated notice", notice)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, []byte(data)) {
				t.Fatal("old history was modified", err)
			}
		})
	}
}

func TestConfiguredPromptUsesNamedStages(t *testing.T) {
	definition := DefaultWorkflow()
	for i := range definition.Stages {
		if definition.Stages[i].Category == "review" {
			definition.Stages[i].Name = "Quality gate"
		}
	}
	current := Team{Workflow: definition}
	options, err := Options(t.Context(), &current, &Member{Role: "worker"}, "continue")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"Quality gate": review`, "task_workflow", "pending_release", "explicit version"} {
		if !strings.Contains(options.System, want) {
			t.Fatalf("missing configured handoff %q", want)
		}
	}
}
