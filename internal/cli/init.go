package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/qivs/agent-deck/internal/gitinfo"
	"github.com/qivs/agent-deck/internal/team"
)

type initFlags struct {
	Directory, Name, Template, Templates, Project, Base, Server, Model string
	ReadOnly, ListTemplates                                            bool
}

func parseInitFlags(args []string) (initFlags, error) {
	var opts initFlags
	f := flag.NewFlagSet("agent-deck init", flag.ContinueOnError)
	f.StringVar(&opts.Directory, "dir", ".", "directory inside the project repository")
	f.StringVar(&opts.Name, "team", "", "private team name (defaults to repository name)")
	f.StringVar(&opts.Template, "template", "custom", "role template name")
	f.StringVar(&opts.Templates, "templates-dir", filepath.Join(filepath.Dir(team.DefaultRoot()), "templates"),
		"user role template directory")
	f.StringVar(&opts.Project, "project", "", "portable project policy file")
	f.StringVar(&opts.Base, "base", "", "override detected default branch")
	f.StringVar(&opts.Server, "server", "", "OpenCode server URL")
	f.StringVar(&opts.Model, "model", "", "default provider/model")
	f.BoolVar(&opts.ReadOnly, "read-only", false, "disable saving")
	f.BoolVar(&opts.ListTemplates, "list-templates", false, "list available role templates")
	if err := f.Parse(args); err != nil {
		return opts, fmt.Errorf("parse init flags: %w", err)
	}
	if f.NArg() != 0 {
		return opts, fmt.Errorf("unexpected arguments: %v", f.Args())
	}
	return opts, nil
}

func initCommand(ctx context.Context, args []string, out io.Writer) error {
	opts, err := parseInitFlags(args)
	if err != nil {
		return err
	}
	templates, err := team.LoadRoleTemplates(ctx, opts.Templates)
	if err != nil {
		return fmt.Errorf("load init templates: %w", err)
	}
	if opts.ListTemplates {
		for _, template := range templates {
			if err := writeText(out, template.Name+"\t"+template.Description); err != nil {
				return err
			}
		}
		return nil
	}
	if opts.ReadOnly {
		return fmt.Errorf("read-only: team initialization disabled")
	}
	t, err := prepareInit(ctx, &opts, templates)
	if err != nil {
		return err
	}
	store := team.Store{Root: team.DefaultRoot()}
	t.ProjectOverlay = false
	if _, err := store.Save(ctx, &t, nil); err != nil {
		return fmt.Errorf("save initialized team: %w", err)
	}
	return writeText(out, fmt.Sprintf("Saved team %s to %s; no sessions created.", t.Name, store.Path(t.Name)))
}

func prepareInit(ctx context.Context, opts *initFlags, templates []team.RoleTemplate) (team.Team, error) {
	root, base, err := detectProjectGit(ctx, opts.Directory)
	if err != nil && opts.Base != "" {
		root, err = projectGit(ctx, opts.Directory, "rev-parse", "--show-toplevel")
		base = opts.Base
	}
	if err != nil {
		return team.Team{}, err
	}
	policy, err := initPolicy(ctx, opts, templates, root)
	if err != nil {
		return team.Team{}, err
	}
	if opts.Base != "" {
		policy.Base = opts.Base
	}
	if opts.Name != "" {
		policy.Name = opts.Name
	}
	if opts.Model != "" {
		policy.Model = opts.Model
	}
	if policy.Base != "" {
		base = policy.Base
	}
	t := team.Team{
		Version: 1, Name: policy.Name, Repo: root, Server: resolveServer(opts.Server, ""), Base: base,
		Model: policy.Model, Capacity: policy.Capacity, Rules: policy.Rules,
	}
	for _, role := range policy.Members {
		t.Members = append(t.Members, team.Member{
			Name: role.Name, Role: role.Role, Mode: "shared", Dir: root,
			Model: role.Model, Agent: role.Agent, System: role.System, Brief: role.Brief,
			Capability: role.Capability, Permissions: team.Preset(role.Capability),
		})
	}
	if opts.Project != "" || projectExists(root) {
		for i := range t.Members {
			member := &t.Members[i]
			if member.Capability == "full" {
				member.Capability = "coder"
				member.Permissions = team.Preset("coder")
			}
		}
	}
	if err := t.Validate(); err != nil {
		return t, fmt.Errorf("validate initialized team: %w", err)
	}
	return t, nil
}

func projectExists(root string) bool {
	_, err := os.Stat(filepath.Join(root, "agent-deck.yaml"))
	return err == nil
}

func initPolicy(ctx context.Context, opts *initFlags, templates []team.RoleTemplate, root string,
) (team.ProjectPolicy, error) {
	project := opts.Project
	if project == "" {
		candidate := filepath.Join(root, "agent-deck.yaml")
		if _, err := os.Stat(candidate); err == nil {
			project = candidate
		} else if !os.IsNotExist(err) {
			return team.ProjectPolicy{}, fmt.Errorf("inspect project policy: %w", err)
		}
	}
	if project != "" {
		policy, err := team.LoadProject(ctx, project)
		if err != nil {
			return policy, fmt.Errorf("load init project: %w", err)
		}
		return policy, nil
	}
	for _, template := range templates {
		if template.Name == opts.Template {
			return team.ProjectPolicy{
				Version: 1, Name: team.Slug(filepath.Base(root)), Capacity: max(1, len(template.Members)-1),
				Members: template.Members,
			}, nil
		}
	}
	return team.ProjectPolicy{}, fmt.Errorf("unknown template %q; use --list-templates", opts.Template)
}

func projectGit(ctx context.Context, dir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	command.Env = gitinfo.Environment()
	data, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("inspect project git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(data)), nil
}

func detectProjectGit(ctx context.Context, dir string) (root, base string, err error) {
	root, err = projectGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	if ref, err := projectGit(ctx, root, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD"); err == nil {
		return root, strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
	}
	for _, candidate := range []string{"main", "master"} {
		if _, err := projectGit(ctx, root, "show-ref", "--verify", "--quiet", "refs/heads/"+candidate); err == nil {
			return root, candidate, nil
		}
	}
	base, err = projectGit(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return root, "", fmt.Errorf("cannot detect default branch from detached HEAD; provide --base: %w", err)
	}
	return root, base, nil
}
