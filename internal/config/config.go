package config

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Agent binds a named dashboard pane to a session and its directory.
type Agent struct {
	CodeDir string `yaml:"code_dir,omitempty" json:"code_dir,omitempty"`
	Name    string `yaml:"name"             json:"name"`
	Session string `yaml:"session"          json:"session"`
	Dir     string `yaml:"dir"              json:"dir"`
	Branch  string `yaml:"branch,omitempty" json:"branch,omitempty"`
	Base    string `yaml:"base,omitempty"   json:"base,omitempty"`
	Role    string `yaml:"role,omitempty"   json:"role,omitempty"`
	Parent  string `yaml:"parent,omitempty" json:"parent,omitempty"`
}

// Deck describes a server and the sessions displayed together.
type Deck struct {
	Server   string  `yaml:"server"             json:"server"`
	Master   Agent   `yaml:"master,omitempty"   json:"master,omitempty"`
	Workers  []Agent `yaml:"workers,omitempty"  json:"workers,omitempty"`
	Sessions []Agent `yaml:"sessions,omitempty" json:"sessions,omitempty"`
}

func (d *Deck) Agents() []Agent {
	var out []Agent
	if d.Master.Session != "" {
		a := d.Master
		a.Role = "lead"
		out = append(out, a)
	}
	out = append(out, d.Workers...)
	return append(out, d.Sessions...)
}

func Parse(data []byte) (Deck, error) {
	var d Deck
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("decode deck: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return d, fmt.Errorf("deck must contain a single YAML/JSON document")
	}
	if d.Server == "" {
		d.Server = "http://127.0.0.1:4096"
	}
	u, err := url.Parse(d.Server)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return d, fmt.Errorf("server must be an HTTP(S) URL")
	}
	if len(d.Agents()) == 0 {
		return d, fmt.Errorf("deck needs at least one session")
	}
	return d, validateAgents(d.Agents())
}

func validateAgents(agents []Agent) error {
	seen := map[string]bool{}
	for i := range agents {
		a := &agents[i]
		if a.Name == "" || a.Session == "" || !filepath.IsAbs(a.Dir) {
			return fmt.Errorf("each agent needs a name, session ID, and absolute dir")
		}
		if a.Role != "" && a.Role != "lead" && a.Role != "master" {
			return fmt.Errorf("unsupported role %q", a.Role)
		}
		key := a.Dir + "\x00" + a.Session
		if seen[key] {
			return fmt.Errorf("duplicate session %s in %s", a.Session, a.Dir)
		}
		seen[key] = true
	}
	return nil
}

func Load(ctx context.Context, path string) (Deck, error) {
	if err := ctx.Err(); err != nil {
		return Deck{}, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Deck{}, fmt.Errorf("read deck %q: %w", path, err)
	}
	return Parse(b)
}

// Write refuses to overwrite a user's deck.
func Write(ctx context.Context, path string, d *Deck) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := yaml.Marshal(d)
	if err != nil {
		return fmt.Errorf("encode deck: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create deck %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	_, err = f.Write(b)
	if err != nil {
		return fmt.Errorf("write deck %q: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close deck %q: %w", path, err)
	}
	return nil
}
