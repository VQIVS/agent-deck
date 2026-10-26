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

// RoleTemplate is a versioned team recipe shared by built-ins and user overrides.
type RoleTemplate struct {
	Version     int          `yaml:"version"`
	Name        string       `yaml:"name"`
	Description string       `yaml:"description"`
	Members     []RolePolicy `yaml:"members"`
}

func (t RoleTemplate) Validate() error {
	if t.Version != 1 || t.Name == "" || Slug(t.Name) != t.Name || strings.TrimSpace(t.Description) == "" {
		return fmt.Errorf("template needs version 1, slug name and description")
	}
	return validateRoles(t.Members)
}

func ParseRoleTemplate(data []byte) (RoleTemplate, error) {
	var t RoleTemplate
	if err := decodePolicy(data, &t); err != nil {
		return t, err
	}
	return t, t.Validate()
}

func templateRole(name, role, capability, system, brief string) RolePolicy {
	return RolePolicy{Name: name, Role: role, Capability: capability, System: system, Brief: brief}
}

func BuiltinRoleTemplates() []RoleTemplate {
	master := templateRole("master", "master", "coder",
		"Coordinate the team, assign scoped work, review changes and verify the final result.",
		"Goal:\nScope:\nAcceptance checks:\n")
	return []RoleTemplate{
		{
			Version: 1, Name: "upgrade-team", Description: "Upgrade dependencies, runtime and CI with verification.",
			Members: []RolePolicy{
				master,
				templateRole("upgrade", "worker", "coder", "Update dependencies and runtime within the assigned scope.",
					"Target versions:\nCompatibility constraints:\n"),
				templateRole("tests", "worker", "coder", "Check upgrade behavior and add focused regression coverage.",
					"Affected behavior:\nTest commands:\n"),
				templateRole("ci", "worker", "coder", "Update CI for the selected runtime and dependency versions.",
					"Supported platforms:\nCI checks:\n"),
			},
		},
		{
			Version: 1, Name: "review-pair", Description: "Implementation paired with independent read-only review.",
			Members: []RolePolicy{
				master,
				templateRole("reviewer", "worker", "reviewer", "Review correctness; report findings with locations.",
					"Review scope:\nRisk areas:\n"),
			},
		},
		{
			Version: 1, Name: "bugfix-tester", Description: "A focused fix with an independent regression tester.",
			Members: []RolePolicy{
				master,
				templateRole("fixer", "worker", "coder", "Reproduce the bug and implement the smallest correct fix.",
					"Reproduction:\nExpected behavior:\n"),
				templateRole("tester", "worker", "coder", "Verify the fix and cover the regression independently.",
					"Regression case:\nVerification commands:\n"),
			},
		},
		{
			Version: 1, Name: "custom", Description: "Start with a coordinator and define your own roles.",
			Members: []RolePolicy{master},
		},
	}
}

// LoadRoleTemplates replaces complete built-ins by name; a missing override directory is allowed.
func LoadRoleTemplates(ctx context.Context, dir string) ([]RoleTemplate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	templates := map[string]RoleTemplate{}
	for _, t := range BuiltinRoleTemplates() {
		templates[t.Name] = t
	}
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read role templates: %w", err)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || (filepath.Ext(entry.Name()) != ".yaml" && filepath.Ext(entry.Name()) != ".yml") {
			continue
		}
		t, err := loadRoleTemplate(ctx, filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if seen[t.Name] {
			return nil, fmt.Errorf("duplicate user template %q", t.Name)
		}
		seen[t.Name], templates[t.Name] = true, t
	}
	result := make([]RoleTemplate, 0, len(templates))
	for _, t := range templates {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func loadRoleTemplate(ctx context.Context, filename string) (RoleTemplate, error) {
	if err := ctx.Err(); err != nil {
		return RoleTemplate{}, err
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return RoleTemplate{}, fmt.Errorf("read role template %q: %w", filename, err)
	}
	t, err := ParseRoleTemplate(data)
	if err != nil {
		return t, fmt.Errorf("parse role template %q: %w", filename, err)
	}
	return t, nil
}

// RolePolicy is one role of a team template.
type RolePolicy struct {
	Name       string `yaml:"name"`
	Role       string `yaml:"role"`
	Capability string `yaml:"capability"`
	Model      string `yaml:"model,omitempty"`
	Agent      string `yaml:"agent,omitempty"`
	System     string `yaml:"system,omitempty"`
	Brief      string `yaml:"brief,omitempty"`
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
