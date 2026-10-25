package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTaskFlagsParallelFileEditPersistence(t *testing.T) {
	run, store := taskCLIFixture(t)
	file := filepath.Join(t.TempDir(), "spec.json")
	write := func(spec string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(spec), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"title":"Original","criteria":["Pass"],"paths":["src/**"],"parallel":true}`)
	taskCLIChange(t, run, "add", "--file", file)
	for _, step := range []struct {
		spec string
		want bool
	}{
		{`{"title":"Rename"}`, true},
		{`{"estimate":"20m","tokens":200}`, true},
		{`{"parallel":false}`, false},
		{`{"description":"Keep serial"}`, false},
		{`{"parallel":true}`, true},
	} {
		write(step.spec)
		taskCLIChange(t, run, "edit", "--id", "T-1", "--file", file)
		loaded, err := store.Load(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if got := loaded.Board.Tasks[0].Estimate.Parallel; got != step.want {
			t.Fatalf("after %s: persisted parallel=%v, want %v", step.spec, got, step.want)
		}
	}
	// Flag-only edits must also preserve the last persisted value.
	taskCLIChange(t, run, "edit", "--id", "T-1", "--title", "Flag-only rename")
	loaded, err := store.Load(t.Context())
	if err != nil || !loaded.Board.Tasks[0].Estimate.Parallel {
		t.Fatalf("flag-only edit lost parallel: %+v, %v", loaded, err)
	}
}
