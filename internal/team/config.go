package team

import (
	"bytes"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/vqivs/agent-deck/internal/config"
)

// Member binds a team role to workspace, session, and creation-time policy.
type Member struct {
	ControlDir           string            `yaml:"control_dir,omitempty"`
	PreviousSession      string            `yaml:"previous_session,omitempty"`
	PreviousDir          string            `yaml:"previous_dir,omitempty"`
	PoolCreated          bool              `yaml:"pool_created,omitempty"`
	ID                   string            `yaml:"id,omitempty"`
	Muted                bool              `yaml:"muted,omitempty"`
	ProjectEditUntrusted bool              `yaml:"-" json:"-"`
	ProjectSystem        string            `yaml:"-" json:"-"`
	Nudge                NudgePolicy       `yaml:"nudge,omitempty"`
	NudgeStatus          NudgeState        `yaml:"nudge_status,omitempty"`
	StallAfter           *time.Duration    `yaml:"stall_after,omitempty"`
	LeasePolicy          string            `yaml:"lease_policy,omitempty"`
	Leases               []string          `yaml:"leases,omitempty"`
	Capability           string            `yaml:"capability,omitempty"`
	Name                 string            `yaml:"name"`
	Role                 string            `yaml:"role"`
	Model                string            `yaml:"model,omitempty"`
	Agent                string            `yaml:"agent,omitempty"`
	System               string            `yaml:"system,omitempty"`
	SystemFile           string            `yaml:"system_file,omitempty"`
	Brief                string            `yaml:"brief,omitempty"`
	Permissions          map[string]string `yaml:"permissions,omitempty"`
	Mode                 string            `yaml:"mode"`
	BranchTemplate       string            `yaml:"branch_template,omitempty"`
	Dir                  string            `yaml:"dir,omitempty"`
	Branch               string            `yaml:"branch,omitempty"`
	Session              string            `yaml:"session,omitempty"`
	Owned                bool              `yaml:"owned_worktree,omitempty"`
}

// Team is a versioned local configuration with exactly one coordinating master.
type Team struct {
	ProjectPrivate   *Team            `yaml:"-" json:"-"`
	ProjectPolicy    *ProjectPolicy   `yaml:"-" json:"-"`
	DefinitionOfDone DefinitionOfDone `yaml:"definition_of_done,omitempty"`
	ProjectOverlay   bool             `yaml:"-" json:"-"`
	ProjectRules     string           `yaml:"-" json:"-"`
	Approval         string           `yaml:"approval,omitempty"`
	SafeCommands     []string         `yaml:"safe_commands,omitempty"`
	LeasePool        bool             `yaml:"lease_pool,omitempty"`
	Version          int              `yaml:"version"`
	Name             string           `yaml:"name"`
	Server           string           `yaml:"server"`
	Repo             string           `yaml:"repo"`
	Base             string           `yaml:"base"`
	Model            string           `yaml:"model,omitempty"`
	Capacity         int              `yaml:"capacity"`
	Rules            string           `yaml:"rules,omitempty"`
	RosterTemplate   string           `yaml:"roster_template,omitempty"`
	Members          []Member         `yaml:"members"`
}

func Slug(s string) string {
	return strings.Trim(regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(strings.ToLower(s), "-"), "-")
}

func Preset(name string) map[string]string {
	if name == "inherited" {
		return nil
	}
	p := map[string]string{}
	for _, k := range []string{"read", "edit", "bash", "webfetch", "external_directory"} {
		p[k] = "ask"
	}
	p["read"] = "allow"
	p["webfetch"] = "allow"
	switch name {
	case "reviewer":
		p["edit"] = "deny"
		p["bash"] = "deny"
		p["external_directory"] = "deny"
	case "coder":
		p["edit"] = "allow"
	case "release":
		p["edit"] = "allow"
		p["bash"] = "allow"
	case "full":
		for k := range p {
			p[k] = "allow"
		}
	}
	return p
}

func memberSlug(name string, taken map[string]bool) (string, error) {
	slug := Slug(name)
	if slug == "" || taken[slug] {
		return "", fmt.Errorf("member names must be unique and nonempty")
	}
	if slug == Slug(AutomationActor.Member) || slug == "user" {
		return "", fmt.Errorf("member name %q is reserved", name)
	}
	return slug, nil
}

func (t *Team) Validate() error {
	if err := t.validatePolicies(); err != nil {
		return err
	}
	if Slug(t.Name) == "" || t.Version != 1 || !filepath.IsAbs(t.Repo) || t.Base == "" || t.Capacity < 1 {
		return fmt.Errorf("team needs version 1, name, absolute repo, base and positive capacity")
	}
	u, e := url.Parse(t.Server)
	if e != nil || !validServer(u) {
		return fmt.Errorf("server must be HTTP(S), without credentials or query")
	}
	masters := 0
	names := map[string]bool{}
	for i := range t.Members {
		m := &t.Members[i]
		if err := m.validate(); err != nil {
			return fmt.Errorf("member %q: %w", m.Name, err)
		}
		slug, err := memberSlug(m.Name, names)
		if err != nil {
			return err
		}
		names[slug] = true
		if m.Role == "master" {
			masters++
		}
	}
	if masters != 1 {
		return fmt.Errorf("exactly one master required")
	}
	_, e = t.Roster("validate")
	return e
}

func validServer(u *url.URL) bool {
	return u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") && u.User == nil && u.RawQuery == ""
}

func (m *Member) validate() error {
	if err := m.validateSessionScope(); err != nil {
		return err
	}
	switch m.Capability {
	case "", "inherited", "reviewer", "coder", "release", "full":
	default:
		return fmt.Errorf("unknown capability preset %q", m.Capability)
	}
	if m.Role != "master" && m.Role != "worker" {
		return fmt.Errorf("member role must be master or worker")
	}
	if m.Mode != "worktree" && m.Mode != "shared" && m.Mode != "adopt" {
		return fmt.Errorf("workspace must be worktree, shared or adopt")
	}
	if m.Mode != "worktree" && !filepath.IsAbs(m.Dir) {
		return fmt.Errorf("shared/adopt directory must be absolute")
	}
	if m.Mode == "adopt" && m.Session == "" {
		return fmt.Errorf("adopt needs a session ID")
	}
	return m.validatePermissions()
}

func (m *Member) validatePermissions() error {
	if m.Capability == "inherited" {
		if len(m.Permissions) != 0 {
			return fmt.Errorf("inherited capability must not record permission rules")
		}
		return nil
	}
	for _, k := range []string{"read", "edit", "bash", "webfetch", "external_directory"} {
		v := m.Permissions[k]
		if v != "allow" && v != "ask" && v != "deny" {
			return fmt.Errorf("permission %s must be allow/ask/deny", k)
		}
	}
	return nil
}

// PermissionSummary describes recorded creation policy, never an adopted
// session's effective rules, which are owned by OpenCode.
func (m *Member) PermissionSummary() string {
	if m.Capability == "inherited" {
		return "inherited from session"
	}
	profile := m.Capability
	if profile == "" {
		profile = "custom permissions"
	}
	if m.Mode == "adopt" {
		return "inherited from session · " + profile + " applies only to newly created sessions"
	}
	return profile + " · creation rules (not live verification)"
}

func Parse(b []byte) (Team, error) {
	var t Team
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if e := d.Decode(&t); e != nil {
		return t, fmt.Errorf("decode team: %w", e)
	}
	err := t.Validate()
	return t, err
}

func (t *Team) Deck() config.Deck {
	d := config.Deck{Server: t.Server}
	for i := range t.Members {
		m := &t.Members[i]
		if m.Session == "" {
			continue
		}
		a := config.Agent{
			Name: m.Name, Session: m.Session, Dir: m.SessionDir(), CodeDir: m.Dir,
			Branch: m.Branch, Base: t.Base,
		}
		if m.Role == "master" {
			a.Role = "lead"
		}
		d.Sessions = append(d.Sessions, a)
	}
	return d
}

func (t *Team) CanStart(index int) error {
	if index < 0 || index >= len(t.Members) {
		return fmt.Errorf("unknown member")
	}
	m := t.Members[index]
	if m.Session != "" {
		return fmt.Errorf("member already started")
	}
	if m.Role == "master" {
		return nil
	}
	n := 0
	for i := range t.Members {
		w := &t.Members[i]
		if w.Role == "worker" && w.Session != "" {
			n++
		}
	}
	if n >= t.Capacity {
		return fmt.Errorf("worker capacity %d reached", t.Capacity)
	}
	return nil
}

const DefaultRoster = `Team {{.Name}} — {{.Reason}}
Base: {{.Base}}
{{range .Members}}{{.Role}} {{.Name}} | session={{.Session}} | dir={{.Dir}} | branch={{.Branch}}
Brief: {{.Brief}}
{{end}}
Use the OpenCode HTTP API at {{.Server}}. For EACH member use its own directory query:
GET /session/status?directory=<urlencoded dir>
GET /permission?directory=<urlencoded dir>
GET /question?directory=<urlencoded dir>
GET /session/<session id>/message?directory=<urlencoded dir>
Never assume status/events are global. Coordinate workers by name and session ID.
`

func (t *Team) Roster(reason string) (string, error) {
	source := t.RosterTemplate
	if source == "" {
		source = DefaultRoster
	}
	tpl, e := template.New("roster").Option("missingkey=error").Parse(source)
	if e != nil {
		return "", fmt.Errorf("parse roster template: %w", e)
	}
	var b bytes.Buffer
	e = tpl.Execute(&b, struct {
		Team
		Reason string
	}{*t, reason})
	if e != nil {
		return "", fmt.Errorf("render roster: %w", e)
	}
	return b.String() + leaseInstructions(t), nil
}
