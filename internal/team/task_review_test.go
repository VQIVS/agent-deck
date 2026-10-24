package team

import (
	"path/filepath"
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

func TestTaskReportMergedDoDRoundTripWithoutAutoCompletion(t *testing.T) {
	board, mutation := workflowFixture(t)
	if err := board.SetAssignee("task", "worker", mutation); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"ready", "in_progress"} {
		if err := board.MoveTask("task", state, mutation); err != nil {
			t.Fatal(err)
		}
	}
	current := &Team{
		Name: "team", Members: []Member{
			{Name: "worker", Role: "worker", Session: "session"},
			{Name: "lead", Role: "master", Session: "lead-session"},
		},
		DefinitionOfDone: DefinitionOfDone{{ID: "lint", Text: "Lint passes", Required: true}},
	}
	report := &DoneReport{
		TaskID: "task", Member: "worker", Session: "session", Summary: "done",
		Checklist: []ReportItem{{ID: "tests", Complete: true}, {ID: "lint", Complete: true}},
		Tests: []ReportTest{
			{CriterionID: "tests", Command: "go test", Outcome: "pass"},
			{CriterionID: "lint", Command: "lint", Outcome: "pass"},
		},
	}
	store := ReportStore{Root: filepath.Join(t.TempDir(), "reports")}
	record, err := store.SubmitTask(t.Context(), current, board, "worker", report, nil)
	if err != nil || len(record.Definition) != 2 {
		t.Fatal("merged report submission failed", record, err)
	}
	if board.Tasks[0].State != "in_progress" {
		t.Fatal("report submission bypassed review")
	}
	loaded, err := store.LoadTask(t.Context(), current, board, "worker", "task")
	if err != nil || len(loaded.Definition) != 2 {
		t.Fatal("merged report load failed", err)
	}
	if err := ValidateTaskCompletionDefinition(&board.Tasks[0], &loaded); err != nil {
		t.Fatal("merged completion definition rejected", err)
	}
	missing := loaded
	missing.Definition = taskDefinition(&board.Tasks[0])
	if err := board.CompleteTaskForTeam("task", &missing, current, mutation); err == nil {
		t.Fatal("missing team criterion completed task")
	}
	if err := board.CompleteTaskForTeam("task", &loaded, current, mutation); err != nil {
		t.Fatal("merged current evidence did not complete task", err)
	}
	if err := board.Validate(); err != nil || board.Tasks[0].State != "done" {
		t.Fatal("merged completion did not remain valid", err)
	}
	current.DefinitionOfDone[0].Text = "Changed policy"
	if _, err := store.LoadTask(t.Context(), current, board, "worker", "task"); err == nil {
		t.Fatal("stale merged definition loaded")
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

func TestTaskEvidenceRequiresMergedPassingReport(t *testing.T) {
	board, m := workflowFixture(t)
	task := &board.Tasks[0]
	task.Assignee, task.Worker = "worker", "worker"
	current := &Team{
		Name: "team", Members: []Member{{Name: "worker", Role: "worker", Session: "session"}},
		DefinitionOfDone: DefinitionOfDone{{ID: "lint", Text: "Lint passes", Required: true}},
	}
	definition, err := TaskReviewDefinition(current, task)
	if err != nil {
		t.Fatal(err)
	}
	evidence := &TaskEvidence{Report: &ReportRecord{
		Team: "team", Member: "worker", Session: "session", Source: "structured", RecordedAt: m.Now,
		ReportedDone: true, Definition: definition,
		Reported: &DoneReport{
			TaskID: "task", Member: "worker", Session: "session", Summary: "done",
			Checklist: []ReportItem{{ID: "tests", Complete: true}, {ID: "lint", Complete: true}},
			Tests: []ReportTest{
				{CriterionID: "tests", Command: "go test", Outcome: "pass"},
				{CriterionID: "lint", Command: "lint", Outcome: "pass"},
			},
		},
	}}
	if err := ValidateTaskEvidence(current, task, evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Git = &ReportMetadata{Dirty: true}
	if err := ValidateTaskEvidence(current, task, evidence); err == nil {
		t.Fatal("undated Git observation accepted")
	}
	evidence.Git.ObservedAt = m.Now
	evidence.Report.Reported.Tests[1].Outcome = "not_run"
	if err := ValidateTaskEvidence(current, task, evidence); err == nil {
		t.Fatal("Git metadata manufactured test success")
	}
	evidence.Report.Reported.Tests[1].Outcome = "pass"
	current.Members[0].Session = "replacement"
	if err := ValidateTaskEvidence(current, task, evidence); err == nil {
		t.Fatal("stale session evidence accepted")
	}
}
