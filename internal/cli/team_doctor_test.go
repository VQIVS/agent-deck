package cli

import (
	"bytes"
	"os"
	"path/filepath"
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
	checks := projectTeamChecks(t.Context(), store, "doctor", "", "")
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
	checks = projectTeamChecks(t.Context(), store, "missing", "", "")
	if !printFindings(&out, checks) {
		t.Fatal("missing team not reported")
	}
}

func TestProjectDoctorValidAndInvalidPolicy(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	project := filepath.Join(t.TempDir(), "agent-deck.yaml")
	data := "version: 1\nname: example\ncapacity: 1\nmembers:\n" +
		"  - name: lead\n    role: master\n    capability: coder\n"
	if err := os.WriteFile(project, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	store := team.Store{Root: team.DefaultRoot(), ReadOnly: true}
	checks := projectTeamChecks(t.Context(), store, "", project, "")
	var out bytes.Buffer
	if printFindings(&out, checks) {
		t.Fatal("valid policy rejected", out.String())
	}
	if err := os.WriteFile(project, []byte(data+"session: forbidden\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !printFindings(&out, projectTeamChecks(t.Context(), store, "", project, "")) {
		t.Fatal("private binding in project policy accepted")
	}
	if _, err := os.Stat(team.DefaultRoot()); !os.IsNotExist(err) {
		t.Fatal("doctor created private configuration", err)
	}
}
