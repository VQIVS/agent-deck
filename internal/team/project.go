package team

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProjectPolicy contains only portable intent; session and workspace bindings are local.
type ProjectPolicy struct {
	Version  int          `yaml:"version"`
	Name     string       `yaml:"name"`
	Base     string       `yaml:"base,omitempty"`
	Model    string       `yaml:"model,omitempty"`
	Capacity int          `yaml:"capacity"`
	Rules    string       `yaml:"rules,omitempty"`
	Approval string       `yaml:"approval,omitempty"`
	Commands []string     `yaml:"safe_commands,omitempty"`
	Members  []RolePolicy `yaml:"members"`
}

// RolePolicy describes a role without session IDs, branches, or filesystem bindings.
type RolePolicy struct {
	Name       string `yaml:"name"`
	Role       string `yaml:"role"`
	Capability string `yaml:"capability"`
	Model      string `yaml:"model,omitempty"`
	Agent      string `yaml:"agent,omitempty"`
	System     string `yaml:"system,omitempty"`
	Brief      string `yaml:"brief,omitempty"`
}

// ProjectBindings supplies private defaults and existing member identities at merge time.
type ProjectBindings struct {
	Repo      string
	Server    string
	Base      string
	Members   map[string]Member
	LeasePool bool
	Local     *Team
}

func decodePolicy(data []byte, out any) error {
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if err := d.Decode(out); err != nil {
		return fmt.Errorf("decode policy: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("policy must contain exactly one YAML document")
	}
	return nil
}

func ParseProject(data []byte) (ProjectPolicy, error) {
	var p ProjectPolicy
	if err := decodePolicy(data, &p); err != nil {
		return p, err
	}
	err := p.Validate()
	return p, err
}

func LoadProject(ctx context.Context, filename string) (ProjectPolicy, error) {
	if err := ctx.Err(); err != nil {
		return ProjectPolicy{}, err
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return ProjectPolicy{}, fmt.Errorf("read project policy %q: %w", filename, err)
	}
	return ParseProject(data)
}

func (p *ProjectPolicy) Validate() error {
	if p.Version != 1 || Slug(p.Name) != p.Name || p.Name == "" || p.Capacity < 1 {
		return fmt.Errorf("project needs version 1, slug name and positive capacity")
	}
	if strings.ContainsAny(p.Base, "\\:~") || filepath.IsAbs(p.Base) || strings.HasPrefix(p.Base, "../") ||
		strings.HasPrefix(p.Base, "-") {
		return fmt.Errorf("project base must be a portable Git ref")
	}
	if p.Approval != "" && p.Approval != "ask" && p.Approval != "allowlist" {
		return fmt.Errorf("approval must be ask or allowlist")
	}
	if _, err := NewCommandAllowlist(p.Commands); err != nil {
		return err
	}
	return validateRoles(p.Members)
}

func validateRoles(roles []RolePolicy) error {
	masters := 0
	names := map[string]bool{}
	for _, role := range roles {
		if role.Name == "" || Slug(role.Name) != role.Name || names[role.Name] {
			return fmt.Errorf("role names must be unique nonempty slugs")
		}
		names[role.Name] = true
		if role.Role != "master" && role.Role != "worker" {
			return fmt.Errorf("role %q must be master or worker", role.Name)
		}
		if role.Role == "master" {
			masters++
		}
		switch role.Capability {
		case "inherited", "reviewer", "coder", "full":
		default:
			return fmt.Errorf("role %q needs a known capability", role.Name)
		}
	}
	if masters != 1 {
		return fmt.Errorf("exactly one master role required")
	}
	return nil
}

func (p *ProjectPolicy) Merge(bindings ProjectBindings) (Team, error) {
	if err := p.Validate(); err != nil {
		return Team{}, err
	}
	t := Team{
		Version: 1, Name: p.Name, Server: bindings.Server, Repo: bindings.Repo,
		Base: bindings.Base, Capacity: p.Capacity, LeasePool: bindings.LeasePool,
	}
	if bindings.Local != nil {
		t = *bindings.Local
	}
	t.ProjectOverlay, t.ProjectRules = true, p.Rules
	if t.Base == "" {
		t.Base = p.Base
	}
	t.Members = mergeProjectMembers(p.Members, bindings)
	if err := t.Validate(); err != nil {
		return Team{}, fmt.Errorf("merge project bindings: %w", err)
	}
	return t, nil
}

func mergeProjectMembers(roles []RolePolicy, bindings ProjectBindings) []Member {
	var out []Member
	if bindings.Local != nil {
		out = append(out, bindings.Local.Members...)
	} else {
		keys := make([]string, 0, len(bindings.Members))
		for key := range bindings.Members {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			out = append(out, bindings.Members[key])
		}
	}
	for _, role := range roles {
		index := -1
		for i := range out {
			if strings.EqualFold(out[i].Name, role.Name) {
				index = i
				break
			}
		}
		if index < 0 {
			if len(bindings.Members) > 0 || bindings.Local != nil {
				continue
			}
			member := Member{
				Name: role.Name, Role: role.Role, Mode: "shared", Dir: bindings.Repo,
				Capability: "coder", Permissions: Preset("coder"),
			}
			out = append(out, member)
			index = len(out) - 1
		}
		member := &out[index]
		member.ProjectSystem = role.System
		if member.Capability != "inherited" {
			member.Permissions = stricterPermissions(member.Permissions, Preset(role.Capability))
		}
	}
	return out
}

func stricterPermissions(local, project map[string]string) map[string]string {
	rank := map[string]int{"allow": 0, "ask": 1, "deny": 2}
	out := map[string]string{}
	for _, key := range []string{"read", "edit", "bash", "webfetch", "external_directory"} {
		value := local[key]
		if value == "" {
			value = "ask"
		}
		if rank[project[key]] > rank[value] {
			value = project[key]
		}
		out[key] = value
	}
	return out
}
