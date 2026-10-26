package cli

import (
	"bytes"
	"os"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestProjectTeamDoctorReadOnlyFailures(t *testing.T) {
	repo := initTestRepo(t)
	var out bytes.Buffer
	if err := initCommand(t.Context(), []string{"--dir", repo, "--team", "doctor"}, &out); err != nil {
		t.Fatal(err)
	}
	store := team.Store{Root: team.DefaultRoot(), ReadOnly: true}
	before, err := os.ReadFile(store.Path("doctor"))
	if err != nil {
		t.Fatal(err)
	}
	checks := projectTeamChecks(t.Context(), store, "doctor", "")
	// An unborn repository has no base commit; doctor must fail rather than invent one.
	if !printFindings(&out, checks) {
		t.Fatal("unborn base not reported")
	}
	after, err := os.ReadFile(store.Path("doctor"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("doctor changed team file", err)
	}
	for _, check := range checks {
		if check.name == "Team repository" && check.fix != "" {
			t.Fatal("valid repository rejected", check)
		}
	}
	checks = projectTeamChecks(t.Context(), store, "missing", "")
	if !printFindings(&out, checks) {
		t.Fatal("missing team not reported")
	}
}
