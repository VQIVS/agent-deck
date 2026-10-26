package team

import (
	"slices"
	"testing"
)

func TestTaskReviewCurrentMembership(t *testing.T) {
	for _, scenario := range []string{
		"unknown", "muted", "not-reviewer", "self", "read-only", "accept", "changes", "user",
	} {
		t.Run(scenario, func(t *testing.T) {
			board, m := workflowFixture(t)
			if err := board.SetAssignee("task", "worker", m); err != nil {
				t.Fatal(err)
			}
			for _, state := range []string{"ready", "in_progress", "review"} {
				if err := board.MoveTask("task", state, m); err != nil {
					t.Fatal(err)
				}
			}
			current := &Team{Members: []Member{{Name: "reviewer", Role: "worker"}, {Name: "worker", Role: "worker"}}}
			name, decision := "reviewer", "accept"
			switch scenario {
			case "unknown":
				name = "missing"
			case "muted":
				current.Members[0].Muted = true
			case "not-reviewer":
				name = "worker"
			case "self":
				board.Tasks[0].Reviewer = "worker"
				name = "worker"
			case "changes":
				decision = "changes"
			}
			err := ReviewTaskForTeam(board, current, "task", name, decision, "review evidence",
				scenario == "user", m.Now, scenario == "read-only")
			allowed := slices.Contains([]string{"accept", "changes", "user"}, scenario)
			if (err == nil) != allowed {
				t.Fatalf("review authorization: %v", err)
			}
			if scenario == "changes" && board.Tasks[0].State != "in_progress" {
				t.Fatal("changes did not resume work")
			}
		})
	}
}

func TestTaskReviewDefinitionMerge(t *testing.T) {
	board, _ := workflowFixture(t)
	current := &Team{DefinitionOfDone: DefinitionOfDone{
		{ID: "tests", Text: "Tests pass", Required: false}, {ID: "lint", Text: "Lint passes", Required: true},
	}}
	definition, err := TaskReviewDefinition(current, &board.Tasks[0])
	if err != nil || len(definition) != 2 || !definition[0].Required || definition[1].ID != "lint" {
		t.Fatal("merge weakened or lost requirements", definition, err)
	}
	current.DefinitionOfDone[0].Text = "Different requirement"
	if _, err := TaskReviewDefinition(current, &board.Tasks[0]); err == nil {
		t.Fatal("conflicting criterion IDs accepted")
	}
}
