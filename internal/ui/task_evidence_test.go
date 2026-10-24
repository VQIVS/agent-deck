package ui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vqivs/agent-deck/internal/gitinfo"
	"github.com/vqivs/agent-deck/internal/team"
)

func TestTaskEvidenceSeparatesClaimsAndCurrentGit(t *testing.T) {
	task := &team.Task{
		ID: "T-1", Branch: "claimed-branch", Commits: []string{"claimed-hash"},
		Paths: team.Leases{"src/**"}, Report: &team.TaskReport{Text: "Finished", Tests: "claimed pass"},
	}
	target := NewTaskEvidenceTarget(task, "/checkout", "main")
	observation := &TaskGitObservation{Target: target, Facts: gitinfo.TaskFacts{
		Branch: "observed-branch", Base: "base-hash",
		Ahead: 2, ObservedAt: time.Unix(1000, 0), Commits: []gitinfo.TaskCommit{{Hash: "observed-hash", Subject: "fix"}},
	}}
	view := TaskEvidenceDetail(task, observation, &target, 120)
	for _, text := range []string{
		"Worker-reported", "claimed-branch", "Tests reported: claimed pass",
		"Independently observed Git", "observed-branch", "observed-hash", "do not verify test outcomes",
	} {
		if !strings.Contains(view, text) {
			t.Fatalf("missing provenance %q: %s", text, view)
		}
	}
	target.Directory = "/different"
	view = TaskEvidenceDetail(task, observation, &target, 120)
	if strings.Contains(view, "observed-branch") || !strings.Contains(view, "Unavailable") {
		t.Fatal("stale checkout evidence shown")
	}
	if task.Branch != "claimed-branch" || task.Commits[0] != "claimed-hash" {
		t.Fatal("observation overwrote claims")
	}
}

func TestTaskEvidenceBindingCloneAndUnavailable(t *testing.T) {
	task := &team.Task{ID: "T-1", Paths: team.Leases{"src/**"}}
	target := NewTaskEvidenceTarget(task, "/repo", "main")
	task.Paths[0] = "other/**"
	if target.Paths[0] != "src/**" {
		t.Fatal("target aliases live task paths")
	}
	if !strings.Contains(TaskEvidenceDetail(task, nil, &target, 80), "Unavailable") {
		t.Fatal("nil observation invented facts")
	}
	observation := ObserveTaskEvidence(t.Context(), nil)
	if observation.Err == nil {
		t.Fatal("missing target accepted")
	}
}

func TestAsyncTaskGitRejectsEveryStaleTargetDimension(t *testing.T) {
	task := &team.Task{ID: "task", Assignee: "worker", Paths: team.Leases{"src/**"}}
	current := &team.Team{Repo: "/repo", Base: "main", Members: []team.Member{
		{Name: "worker", Dir: "/worktree", ControlDir: "/private/control"},
	}}
	target := CurrentTaskEvidenceTarget(current, task)
	if target.Directory != "/worktree" {
		t.Fatal("Git observation used session control directory")
	}
	result := &taskGitResult{TaskID: task.ID, Target: target}
	if !CurrentTaskGitResult(result, &target) {
		t.Fatal("matching result rejected")
	}
	for _, dimension := range []string{"task", "directory", "base", "paths"} {
		changed := target
		switch dimension {
		case "task":
			changed.TaskID = "other"
		case "directory":
			changed.Directory = "/different"
		case "base":
			changed.Base = "release"
		case "paths":
			changed.Paths = []string{"other/**"}
		}
		if CurrentTaskGitResult(result, &changed) {
			t.Errorf("stale %s result accepted", dimension)
		}
	}
	observation := &TaskGitObservation{Target: target, Facts: gitinfo.TaskFacts{
		ObservedAt: time.Now(), Branch: "old-observed-branch",
	}}
	task.Paths = team.Leases{"changed/**"}
	if strings.Contains(TaskEvidenceDetail(task, observation, &target, 120), "old-observed-branch") {
		t.Fatal("old captured target displayed after task path edit")
	}
}

func TestApplyTaskGitResultRejectsStaleAndCapturesSlices(t *testing.T) {
	m := &Model{teams: &teamUI{Manager: &team.Manager{Team: team.Team{Name: "pool", Repo: "/repo", Base: "main"}}}}
	m.board.State.Board.Tasks = []team.Task{{ID: "task", Paths: team.Leases{"src/**"}}}
	target := CurrentTaskEvidenceTarget(&m.teams.Manager.Team, &m.board.State.Board.Tasks[0])
	result := &taskGitResult{TaskID: "task", Target: target, Facts: gitinfo.TaskFacts{
		ObservedAt: time.Now(), Commits: []gitinfo.TaskCommit{{Hash: "hash"}},
	}}
	m.applyTaskGitResult(result)
	if m.board.Git["task"] == nil {
		t.Fatal("current observation not retained")
	}
	result.Target.Paths[0] = "changed/**"
	result.Facts.Commits[0].Hash = "changed"
	if m.board.Git["task"].Target.Paths[0] != "src/**" || m.board.Git["task"].Facts.Commits[0].Hash != "hash" {
		t.Fatal("stored observation aliases caller slices")
	}
	m.applyTaskGitResult(result)
	if m.board.Git["task"].Target.Paths[0] != "src/**" {
		t.Fatal("stale result replaced current observation")
	}
	target.Base = "release"
	if strings.Contains(strings.Join(CurrentTaskGitLines(m.board.Git["task"], &target), "\n"), "hash") {
		t.Fatal("stale observation rendered after base changed")
	}
}

func TestTeamDoDStrictRoundTrip(t *testing.T) {
	definition := team.DefinitionOfDone{
		{ID: "tests", Text: "Package tests pass: including race", Required: true},
		{ID: "docs", Text: "Update docs if needed", Required: false},
	}
	text, err := FormatTeamDoD(definition)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseTeamDoD(text)
	if err != nil || !reflect.DeepEqual(parsed, definition) {
		t.Fatalf("round trip: %+v %v", parsed, err)
	}
	for _, invalid := range []string{
		"- id: tests\n  text: Pass\n  required: true\n  unknown: ignored",
		"- id: tests\n  text: Pass\n---\n- id: other\n  text: Other",
		"- id: duplicate\n  text: One\n- id: duplicate\n  text: Two",
		"- id: empty\n  text: ''", "id: not-a-list", "[unclosed",
	} {
		if _, err := ParseTeamDoD(invalid); err == nil {
			t.Errorf("unsafe DoD accepted: %s", invalid)
		}
	}
	for _, empty := range []string{"", "   ", "[]"} {
		if parsed, err := ParseTeamDoD(empty); err != nil || len(parsed) != 0 {
			t.Fatal("empty policy failed")
		}
	}
}

func TestTeamDoDInvalidFormDoesNotMutate(t *testing.T) {
	draft := &team.Team{DefinitionOfDone: team.DefinitionOfDone{{ID: "old", Text: "Keep policy", Required: true}}}
	before := append(team.DefinitionOfDone(nil), draft.DefinitionOfDone...)
	if ApplyTeamDoDForm(draft, "invalid", false) == nil || !reflect.DeepEqual(before, draft.DefinitionOfDone) {
		t.Fatal("invalid form changed policy")
	}
	if ApplyTeamDoDForm(draft, "", true) == nil || !reflect.DeepEqual(before, draft.DefinitionOfDone) {
		t.Fatal("read-only cleared policy")
	}
	if err := ApplyTeamDoDForm(draft, "", false); err != nil || len(draft.DefinitionOfDone) != 0 {
		t.Fatal("clear policy failed")
	}
}
