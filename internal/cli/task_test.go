package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vqivs/agent-deck/internal/server"
	"github.com/vqivs/agent-deck/internal/team"
)

type taskCLIResult struct {
	Board   team.TaskBoard `json:"board"`
	Warning string         `json:"warning"`
}

func taskCLIFixture(t *testing.T) (func(...string) (string, error), team.BoardStore) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AGENT_DECK_SERVER", "")
	repo := t.TempDir()
	configured := team.Team{
		Version: 1, Name: "task-test", Server: "http://127.0.0.1:1", Repo: repo, Base: "main", Capacity: 2,
		Members: []team.Member{
			{Name: "Master", Role: "master", Mode: "shared", Dir: repo, Capability: "inherited"},
			{Name: "Worker", Role: "worker", Mode: "shared", Dir: repo, Capability: "inherited"},
		},
	}
	if _, err := (team.Store{Root: team.DefaultRoot()}).Save(t.Context(), &configured, nil); err != nil {
		t.Fatal(err)
	}
	root, err := server.StateRoot()
	if err != nil {
		t.Fatal(err)
	}
	store := team.BoardStore{Root: filepath.Join(root, "boards"), Team: configured.Name}
	run := func(args ...string) (string, error) {
		var out bytes.Buffer
		args = append(args, "--team", configured.Name)
		err := taskCommand(t.Context(), args, &out)
		return out.String(), err
	}
	return run, store
}

func taskCLIChange(t *testing.T, run func(...string) (string, error), args ...string) taskCLIResult {
	t.Helper()
	args = append(args, "--json")
	output, err := run(args...)
	if err != nil {
		t.Fatalf("task %v failed: %v; output=%s", args, err, output)
	}
	var result taskCLIResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("invalid mutation JSON: %s %v", output, err)
	}
	return result
}

func TestTaskCLIWorkflowJSONAndAttributedHistory(t *testing.T) {
	run, store := taskCLIFixture(t)
	output, err := run("ls", "--json", "--read-only")
	if err != nil || strings.TrimSpace(output) != "[]" {
		t.Fatalf("empty list: %q %v", output, err)
	}
	created := taskCLIChange(t, run, "add", "--title", "Fix workflow", "--description", "CLI regression",
		"--criteria", "Tests pass", "--criteria", "Review complete", "--path", "src/**", "--path", "tests/**",
		"--label", "workflow", "--label", "cli", "--estimate", "10m", "--priority", "high")
	if len(created.Board.Tasks) != 1 {
		t.Fatal("task not created")
	}
	id := created.Board.Tasks[0].ID
	if created.Board.Tasks[0].State != "backlog" || len(created.Board.Tasks[0].Criteria) != 2 ||
		len(created.Board.Tasks[0].Paths) != 2 || created.Board.Tasks[0].Estimate.Duration.String() != "10m0s" {
		t.Fatalf("flags not preserved: %+v", created.Board.Tasks[0])
	}
	taskCLIChange(t, run, "edit", "--id", id, "--title", "Fix workflow safely", "--priority", "urgent")
	taskCLIChange(t, run, "assign", "--id", id, "--assignee", "Worker", "--reviewer", "Master")
	taskCLIChange(t, run, "comment", "--id", id, "--text", "Please verify race checks")
	taskCLIChange(t, run, "move", "--id", id, "--state", "ready")
	started := taskCLIChange(t, run, "move", "--id", id, "--state", "in_progress")
	if !strings.Contains(started.Warning, "skipped") {
		t.Fatalf("missing explicit notice fallback: %+v", started)
	}
	taskCLIChange(t, run, "move", "--id", id, "--state", "review")
	changes := taskCLIChange(t, run, "review", "--id", id, "--decision", "changes", "--comment", "Add regression coverage")
	if changes.Board.Tasks[0].State != "in_progress" {
		t.Fatal("review changes did not return to work")
	}
	taskCLIChange(t, run, "move", "--id", id, "--state", "review")
	done := taskCLIChange(t, run, "review", "--id", id, "--decision", "accept", "--comment", "Verified checks and scope")
	if done.Board.Tasks[0].State != "done" || done.Board.Tasks[0].Review == nil {
		t.Fatal("accepted review not saved")
	}
	output, err = run("show", "--id", id, "--json", "--read-only")
	var shown team.Task
	if err != nil || json.Unmarshal([]byte(output), &shown) != nil ||
		shown.ID != id || shown.Title != "Fix workflow safely" {
		t.Fatalf("show: %s %v", output, err)
	}
	output, err = run("ls", "--json")
	var listed []team.Task
	if err != nil || json.Unmarshal([]byte(output), &listed) != nil || len(listed) != 1 {
		t.Fatal("list JSON invalid")
	}
	output, err = run("ls")
	if err != nil || !strings.Contains(output, id+"\tdone\tWorker\tFix workflow safely") {
		t.Fatalf("text list: %q %v", output, err)
	}
	stored, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range stored.Board.History {
		if event.Actor != "user" || event.At.IsZero() {
			t.Fatalf("history not trusted user/time: %+v", event)
		}
	}
}

func TestTaskCLIReadOnlyAndInvalidMutationsDoNotPersist(t *testing.T) {
	run, store := taskCLIFixture(t)
	created := taskCLIChange(t, run, "add", "--title", "Protected", "--criteria", "Tests pass", "--path", "src/**")
	id := created.Board.Tasks[0].ID
	before, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"add", "--title", "Forbidden", "--read-only"},
		{"edit", "--id", id, "--title", "Forbidden", "--read-only"},
		{"move", "--id", id, "--state", "ready", "--read-only"},
		{"assign", "--id", id, "--assignee", "Worker", "--read-only"},
		{"comment", "--id", id, "--text", "Forbidden", "--read-only"},
		{"review", "--id", id, "--decision", "accept", "--comment", "Forbidden", "--read-only"},
		{"move", "--id", id, "--state", "done"},
		{"assign", "--id", id, "--assignee", "missing-member"},
		{"review", "--id", id, "--decision", "accept", "--comment", "Not in review"},
	} {
		if output, err := run(args...); err == nil {
			t.Fatalf("invalid mutation accepted: %v output=%s", args, output)
		}
		after, err := store.Load(t.Context())
		if err != nil || after.Revision != before.Revision || after.Board.Tasks[0].Title != "Protected" {
			t.Fatalf("rejected command persisted: %v %+v %v", args, after, err)
		}
	}
	if _, err := run("show", "--id", "not-found"); err == nil {
		t.Fatal("unknown task shown")
	}
}

func TestTaskCLIFileSpecOverridesDependenciesAndStrictInput(t *testing.T) {
	run, _ := taskCLIFixture(t)
	dependency := taskCLIChange(t, run, "add", "--title", "Prerequisite", "--criteria", "Reviewed", "--path", "prereq/**")
	filename := filepath.Join(t.TempDir(), "task.json")
	spec := `{"title":"From file","description":"Loaded JSON","criteria":["Tests pass"],"paths":["file/**"],` +
		`"labels":["file"],"priority":"low","estimate":"15m"}`
	if err := os.WriteFile(filename, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	result := taskCLIChange(t, run, "add", "--file", filename, "--title", "Flag wins", "--label", "override",
		"--dependency", dependency.Board.Tasks[0].ID)
	task := result.Board.Tasks[1]
	if task.Title != "Flag wins" || len(task.Labels) != 1 || task.Labels[0] != "override" || len(task.Dependencies) != 1 {
		t.Fatalf("file/flag merge incorrect: %+v", task)
	}
	if _, err := run("move", "--id", task.ID, "--state", "ready"); err == nil {
		t.Fatal("unfinished dependency ignored")
	}
	for _, invalid := range []string{`{"title":"bad","unknown":true}`, `{"title":"first"} {"title":"second"}`} {
		if err := os.WriteFile(filename, []byte(invalid), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := run("add", "--file", filename); err == nil {
			t.Fatal("invalid task file accepted")
		}
	}
}

func TestTaskCLITextNoticeFallbackIsExplicit(t *testing.T) {
	run, _ := taskCLIFixture(t)
	created := taskCLIChange(t, run, "add", "--title", "Start worker", "--criteria", "Pass", "--path", "src/**",
		"--assignee", "Worker", "--reviewer", "Master", "--ready")
	output, err := run("move", "--id", created.Board.Tasks[0].ID, "--state", "in_progress")
	if err != nil || !strings.Contains(output, "Task change saved") || !strings.Contains(output, "Warning:") ||
		!strings.Contains(output, "skipped") {
		t.Fatalf("notice skip hidden: %q %v", output, err)
	}
}

func TestTaskCLIServerOverrideDoesNotMutateSavedTeam(t *testing.T) {
	run, _ := taskCLIFixture(t)
	calls := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer fake.Close()
	created := taskCLIChange(t, run, "add", "--title", "Override read", "--criteria", "Pass", "--path", "src/**")
	output, err := run("show", "--id", created.Board.Tasks[0].ID, "--server", fake.URL, "--json")
	if err != nil || !json.Valid([]byte(output)) || calls != 0 {
		t.Fatalf("local task read contacted server: %q %v", output, err)
	}
	loaded, _, err := (team.Store{Root: team.DefaultRoot()}).Load(t.Context(), "task-test")
	if err != nil || loaded.Server == fake.URL {
		t.Fatalf("server override persisted: %s %v", loaded.Server, err)
	}
}
