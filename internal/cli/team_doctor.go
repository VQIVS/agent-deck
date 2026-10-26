package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/vqivs/agent-deck/internal/team"
)

// projectTeamChecks performs local read-only checks, returning findings compatible with doctor.
func projectTeamChecks(ctx context.Context, store team.Store, name, directory string) []finding {
	checks := []finding{}
	if name == "" {
		if directory != "" {
			root, _, err := detectProjectGit(ctx, directory)
			checks = append(checks, localCheck("Project repository", root, "Select a Git repository with --dir", err))
		}
		return checks
	}
	t, _, err := store.Load(ctx, name)
	checks = append(checks, localCheck("Team", name, "Correct or initialize the private team configuration", err))
	if err != nil {
		return checks
	}
	checks = append(checks, teamRepositoryChecks(ctx, &t)...)
	for i := range t.Members {
		member := &t.Members[i]
		if member.SystemFile != "" {
			info, err := os.Stat(member.SystemFile)
			if err == nil && !info.Mode().IsRegular() {
				err = fmt.Errorf("system prompt must be a regular file")
			}
			checks = append(checks, localCheck("System prompt "+member.Name, member.SystemFile,
				"Correct the member system prompt file", err))
		}
		if t.LeasePool && member.Session != "" {
			var policyErr error
			if member.LeasePolicy == "" || member.LeasePolicy != t.LeaseFingerprint(member) {
				policyErr = fmt.Errorf("%s", t.LeaseStatus(member))
			}
			checks = append(checks, localCheck("Lease policy "+member.Name, t.LeaseStatus(member),
				"Explicitly apply the configured leases to this member", policyErr))
		}
	}
	return checks
}

func teamRepositoryChecks(ctx context.Context, t *team.Team) []finding {
	root, err := projectGit(ctx, t.Repo, "rev-parse", "--show-toplevel")
	if err == nil {
		actual, actualErr := filepath.EvalSymlinks(root)
		configured, configuredErr := filepath.EvalSymlinks(t.Repo)
		err = errors.Join(actualErr, configuredErr)
		if err == nil && actual != configured {
			err = fmt.Errorf("configured repository is not the Git root %q", root)
		}
	}
	checks := []finding{localCheck("Team repository", t.Repo, "Set the team repository to its Git root", err)}
	if err == nil {
		_, err = projectGit(ctx, t.Repo, "rev-parse", "--verify", "--end-of-options", t.Base+"^{commit}")
		checks = append(checks, localCheck("Team base", t.Base, "Select an existing base commit or branch", err))
	}
	return checks
}

func localCheck(name, detail, fix string, err error) finding {
	if err != nil {
		return finding{name: name, detail: err.Error(), fix: fix}
	}
	return finding{name: name, detail: detail}
}
