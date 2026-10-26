package team

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func example() Team {
	return Team{
		Version:  1,
		Name:     "Example",
		Server:   "http://localhost:4096",
		Repo:     "/tmp/project",
		Base:     "main",
		Capacity: 1,
		Rules:    "Shared rules",
		Members: []Member{
			{Name: "Lead", Role: "master", Mode: "shared", Dir: "/tmp/project", Permissions: Preset("coder")},
			{
				Name:        "Tests",
				Role:        "worker",
				Mode:        "shared",
				Dir:         "/tmp/tests",
				Brief:       "Run tests",
				Permissions: Preset("reviewer"),
			},
		},
	}
}

func TestRoundTripAtomicHistoryConflict(t *testing.T) {
	s := Store{Root: t.TempDir()}
	team := example()
	var version []byte
	for i := 0; i < 8; i++ {
		team.Rules = fmt.Sprint(i)
		v, e := s.Save(t.Context(), &team, version)
		if e != nil {
			t.Fatal(e)
		}
		version = v
		got, b, e := s.Load(t.Context(), team.Name)
		if e != nil || got.Rules != team.Rules || !bytes.Equal(b, version) {
			t.Fatalf("%+v %v", got, e)
		}
	}
	files, _ := filepath.Glob(filepath.Join(s.Root, ".history", "example", "*.yaml"))
	if len(files) != 5 {
		t.Fatal("history", files)
	}
	if _, e := s.Save(t.Context(), &team, []byte("stale")); e == nil {
		t.Fatal("clobbered external edit")
	}
	if tmp, _ := filepath.Glob(filepath.Join(s.Root, ".team-*")); len(tmp) > 0 {
		t.Fatal("temporary save leaked")
	}
	info, _ := os.Stat(s.Path(team.Name))
	if info.Mode().Perm() != 0o600 {
		t.Fatal(info.Mode())
	}
	s.ReadOnly = true
	if _, e := s.Save(t.Context(), &team, version); e == nil {
		t.Fatal("read-only save")
	}
	if e := s.Delete(t.Context(), team.Name); e == nil {
		t.Fatal("read-only delete")
	}
	team.Members[1].Role = "master"
	if team.Validate() == nil {
		t.Fatal("two masters")
	}
	team = example()
	team.Server = "http://user:secret@localhost"
	if team.Validate() == nil {
		t.Fatal("stored credentials")
	}
}

func TestRosterCapacityAndWorkspace(t *testing.T) {
	team := example()
	team.Members[1].Session = "session-worker"
	team.Members[1].Branch = "example/tests"
	text, e := team.Roster("new worker")
	if e != nil || !strings.Contains(text, "session-worker") || !strings.Contains(text, "Run tests") ||
		!strings.Contains(text, "?directory=") {
		t.Fatal(text, e)
	}
	team.Members = append(team.Members, Member{Name: "Extra", Role: "worker"})
	if team.CanStart(2) == nil {
		t.Fatal("capacity ignored")
	}
	if team.CanStart(0) != nil {
		t.Fatal("master consumes capacity")
	}
	team.RosterTemplate = "{{.Name}}: {{.Reason}}"
	if text, e = team.Roster("changed"); e != nil || text != "Example: changed" {
		t.Fatal(text, e)
	}
}

func TestManagerFakeServer(t *testing.T) {
	var requests []string
	var prompts []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		if r.URL.Query().Get("directory") == "" {
			t.Error("unscoped")
		}
		switch {
		case r.URL.Path == "/session":
			var v struct {
				Permission []struct{ Permission, Action string }
			}
			_ = json.NewDecoder(r.Body).Decode(&v)
			if len(v.Permission) != 10 { // 5 preset keys, 3 search tools following read, 2 todo tools
				t.Error("missing rules")
			}
			fmt.Fprint(w, `{"id":"session-new"}`)
		case strings.HasSuffix(r.URL.Path, "prompt_async"):
			var v map[string]any
			_ = json.NewDecoder(r.Body).Decode(&v)
			prompts = append(prompts, v)
			w.WriteHeader(204)
		case strings.HasSuffix(r.URL.Path, "abort"):
			fmt.Fprint(w, "true")
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	team := example()
	team.Server = s.URL
	team.Model = "provider/model"
	team.Members[0].Session = "session-master"
	team.Members[1].System = "Worker system"
	store := Store{Root: t.TempDir()}
	v, e := store.Save(t.Context(), &team, nil)
	if e != nil {
		t.Fatal(e)
	}
	m := Manager{Store: store, Team: team, Version: v}
	ctx := context.Background()
	if e = m.Start(ctx, 1); e != nil {
		t.Fatal(e)
	}
	if m.Team.Members[1].Session != "session-new" || len(prompts) != 2 ||
		!strings.Contains(prompts[0]["system"].(string), "Worker system") {
		t.Fatal(prompts)
	}
	if model := prompts[0]["model"].(map[string]any); model["providerID"] != "provider" || model["modelID"] != "model" {
		t.Fatal(model)
	}
	if e = m.Remove(ctx, 1); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(strings.Join(requests, "\n"), "/abort") || len(m.Team.Members) != 1 {
		t.Fatal(requests)
	}
	count := len(requests)
	m.ReadOnly = true
	if m.Start(ctx, 0) == nil || m.ChangeMaster(ctx, 0) == nil || m.Send(ctx, 0, "hello") == nil {
		t.Fatal("read-only")
	}
	if len(requests) != count {
		t.Fatal("mutated server")
	}
	b, _ := yaml.Marshal(m.Team)
	if _, e = Parse(b); e != nil {
		t.Fatal(e)
	}
}
