package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qivs/agent-deck/internal/team"
)

func TestProjectMergeRetainsPrivateBindings(t *testing.T) {
	repo := t.TempDir()
	data := `version: 1
name: project
capacity: 1
approval: allowlist
safe_commands: ["git status"]
members:
  - name: master
    role: master
    capability: coder
`
	if err := os.WriteFile(filepath.Join(repo, "agent-deck.yaml"), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	local := team.Team{
		Name: "private", Repo: repo, Server: "http://localhost:4096", Base: "main", LeasePool: true,
		Members: []team.Member{{Name: "master", Role: "master", Mode: "adopt", Session: "existing", Dir: repo}},
	}
	if err := mergeProject(t.Context(), &local); err != nil {
		t.Fatal(err)
	}
	if local.Name != "private" || local.Members[0].Session != "existing" ||
		local.Approval != "allowlist" || !local.LeasePool {
		t.Fatal(local)
	}
}
