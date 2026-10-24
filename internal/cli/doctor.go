package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

type diagnostics struct {
	server  string
	command func(context.Context, string, ...string) ([]byte, error)
	health  func(context.Context) error
	models  func(context.Context) (bool, error)
	write   func(context.Context) error
}

type finding struct {
	name, detail, fix string
}

func doctor(args []string) error {
	f := flag.NewFlagSet("doctor", flag.ContinueOnError)
	server := f.String("server", "", "OpenCode server URL")
	name := f.String("team", "", "check saved team")
	project := f.String("project", "", "check portable project file")
	directory := f.String("dir", "", "check project repository")
	if err := f.Parse(args); err != nil {
		return fmt.Errorf("parse doctor flags: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	checks := newDiagnostics(resolveServer(*server, ""))
	findings := checks.check(ctx, true)
	findings = append(findings, projectTeamChecks(ctx, team.Store{Root: team.DefaultRoot(), ReadOnly: true},
		*name, *project, *directory)...)
	if printFindings(os.Stdout, findings) {
		return fmt.Errorf("setup needs attention; run the fix commands above")
	}
	return nil
}

func newDiagnostics(server string) diagnostics {
	client := opencode.New(server, true)
	return diagnostics{
		server: server,
		command: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			out, err := exec.CommandContext(ctx, name, args...).Output()
			if err != nil {
				return nil, fmt.Errorf("run %s: %w", name, err)
			}
			return out, nil
		},
		health: client.Health,
		models: client.HasModels,
		write:  writableConfig,
	}
}

func (d diagnostics) check(ctx context.Context, full bool) []finding {
	version, err := d.command(ctx, "opencode", "--version")
	checks := []finding{{name: "OpenCode", detail: strings.TrimSpace(string(version))}}
	if err != nil {
		checks[0] = finding{"OpenCode", "not available on PATH", "brew install opencode"}
	} else if !supportedVersion(string(version)) {
		checks[0].detail = "requires OpenCode >= " + opencode.MinimumVersion + "; found " + checks[0].detail
		checks[0].fix = "brew upgrade opencode  # or: opencode upgrade (official script install)"
	}
	server := finding{name: "Server", detail: "reachable"}
	if err := d.health(ctx); err != nil {
		server.detail = "not reachable; check --server or AGENT_DECK_SERVER"
		server.fix = serverFix(d.server)
	}
	checks = append(checks, server)
	if full {
		checks = append(checks, d.fullChecks(ctx)...)
	}
	return checks
}

func serverFix(server string) string {
	u, err := url.Parse(server)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1") {
		return "opencode serve --hostname 127.0.0.1 --port 4096; agent-deck --server http://127.0.0.1:4096"
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	return "opencode serve --hostname 127.0.0.1 --port " + port
}

func (d diagnostics) fullChecks(ctx context.Context) []finding {
	checks := []finding{
		{name: "Models", detail: "available"},
		{name: "Git", detail: "available"},
		{name: "Configuration", detail: "writable"},
	}
	if ok, err := d.models(ctx); err != nil || !ok {
		checks[0].detail, checks[0].fix = "no accessible models", "opencode auth login && opencode models"
	}
	if _, err := d.command(ctx, "git", "--version"); err != nil {
		checks[1].detail, checks[1].fix = "not available on PATH", "brew install git  # Linux: sudo apt install git"
	}
	if err := d.write(ctx); err != nil {
		checks[2].detail = err.Error()
		checks[2].fix = fmt.Sprintf("mkdir -p %q && chmod u+rwx %q", team.DefaultRoot(), team.DefaultRoot())
	}
	return checks
}

func printFindings(out io.Writer, checks []finding) bool {
	failed := false
	for _, check := range checks {
		status := "OK"
		if check.fix != "" {
			status, failed = "FIX", true
		}
		_, _ = fmt.Fprintf(out, "%s  %s: %s\n", status, check.name, check.detail)
		if check.fix != "" {
			_, _ = fmt.Fprintf(out, "     %s\n", check.fix)
		}
	}
	return failed
}

func supportedVersion(text string) bool {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 1 {
		return false
	}
	parts := strings.Split(strings.TrimPrefix(fields[0], "v"), ".")
	if len(parts) != 3 {
		return false
	}
	minimum := []int{1, 19, 0}
	var version [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return false
		}
		version[i] = n
	}
	for i, n := range version {
		if n != minimum[i] {
			return n > minimum[i]
		}
	}
	return true
}

func writableConfig(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	root := filepath.Dir(team.DefaultRoot())
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	file, err := os.CreateTemp(root, ".doctor-*")
	if err != nil {
		return fmt.Errorf("write configuration directory: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := file.Close(); err != nil {
		return fmt.Errorf("close configuration probe: %w", err)
	}
	return nil
}
