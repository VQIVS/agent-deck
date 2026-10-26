package team

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ImportOptions supplies local bindings for a portable template. Import never
// contacts the server, creates resources, or sends a roster.
type ImportOptions struct{ Name, Repo string }

type transfer struct {
	Format           string `yaml:"format"`
	Portable         bool   `yaml:"portable"`
	PermissionPolicy string `yaml:"permission_policy,omitempty"`
	Team             Team   `yaml:"team"`
}

func decodeTransfer(b []byte, target any) error {
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("decode transfer: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		return fmt.Errorf("expected one YAML/JSON document")
	}
	return nil
}

func Import(b []byte, opts ImportOptions) (Team, error) {
	var keys map[string]any
	if err := yaml.Unmarshal(b, &keys); err != nil {
		return Team{}, fmt.Errorf("decode import header: %w", err)
	}
	var t Team
	portable := false
	if _, ok := keys["format"]; ok {
		var v transfer
		if err := decodeTransfer(b, &v); err != nil {
			return t, err
		}
		if v.Format != "agent-deck/team" {
			return t, fmt.Errorf("unknown export format %q", v.Format)
		}
		t, portable = v.Team, v.Portable
	} else if _, ok := keys["members"]; ok {
		if err := decodeTransfer(b, &t); err != nil {
			return t, err
		}
	} else {
		return t, fmt.Errorf("unsupported import: use a team file or agent-deck/team export; " +
			"legacy deck files are no longer supported")
	}
	if opts.Name != "" {
		t.Name = opts.Name
	}
	if portable {
		if err := bindPortable(&t, opts.Repo); err != nil {
			return t, err
		}
	}
	for i := range t.Members {
		m := &t.Members[i]
		m.ID = ""
		if m.Capability != "" && m.Capability != "inherited" && len(m.Permissions) == 0 {
			m.Permissions = Preset(m.Capability)
		}
	}
	err := t.Validate()
	return t, err
}

func bindPortable(t *Team, path string) error {
	if path == "" {
		return fmt.Errorf("portable template needs a local repository (--repo)")
	}
	repo, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve import repository: %w", err)
	}
	t.Repo = repo
	for i := range t.Members {
		m := &t.Members[i]
		if m.Mode == "shared" {
			m.Dir = repo
		}
	}
	return nil
}

// Export copies all nested mutable state before stripping machine bindings.
func Export(source *Team, format string, portable bool) ([]byte, error) {
	t := *source
	if err := t.Validate(); err != nil {
		return nil, err
	}
	t.Members = append([]Member(nil), t.Members...)
	for i := range t.Members {
		t.Members[i].Leases = append([]string(nil), t.Members[i].Leases...)
	}
	if portable {
		t.Repo = ""
		for i := range t.Members {
			m := &t.Members[i]
			m.Session, m.Dir, m.Branch = "", "", ""
			m.LeasePolicy = ""
			m.ID = ""
			m.ControlDir, m.PreviousSession, m.PreviousDir = "", "", ""
			m.NudgeStatus = yaml.Node{}
			if m.Mode == "adopt" {
				m.Mode = "shared"
			}
			if filepath.IsAbs(m.SystemFile) {
				m.SystemFile = ""
			}
		}
	}
	b, err := yaml.Marshal(
		transfer{
			Format:   "agent-deck/team",
			Portable: portable,
			PermissionPolicy: "inherited = inherited from session (no rules recorded); " +
				"explicit profiles apply only to newly created sessions",
			Team: t,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("encode team export: %w", err)
	}
	switch format {
	case "yaml":
		return b, nil
	case "json":
		var v any
		if err := yaml.Unmarshal(b, &v); err != nil {
			return nil, fmt.Errorf("decode export for JSON: %w", err)
		}
		b, err = json.MarshalIndent(v, "", "  ")
		return append(b, '\n'), err
	default:
		return nil, fmt.Errorf("format must be yaml or json")
	}
}

// Preview includes the complete proposed configuration and the previous version
// on replacement, making prompt/permission changes visible before confirmation.
func Preview(t *Team, old []byte) string {
	b, _ := yaml.Marshal(t)
	var s strings.Builder
	if len(old) > 0 {
		s.WriteString("REPLACE existing team (backup retained)\n--- Before\n")
		s.Write(old)
		s.WriteString("\n+++ After\n")
	} else {
		s.WriteString("CREATE managed team\n")
	}
	s.Write(b)
	for i := range t.Members {
		member := &t.Members[i]
		fmt.Fprintf(&s, "\n%s: %s", member.Name, member.PermissionSummary())
	}
	s.WriteString("\nLocal configuration only. No session/worktree creation or messages.\n")
	return s.String()
}

// WriteExport never overwrites without an explicit confirmation by its caller.
func WriteExport(ctx context.Context, path string, b []byte, replace bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if replace {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return fmt.Errorf("open export %q: %w", path, err)
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	ce := f.Close()
	if err == nil {
		err = ce
	}
	if err != nil {
		return fmt.Errorf("write export %q: %w", path, err)
	}
	return nil
}
