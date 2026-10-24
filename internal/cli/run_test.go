package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/vqivs/agent-deck/internal/team"
)

func TestServerPrecedence(t *testing.T) {
	t.Setenv("AGENT_DECK_SERVER", "http://env")
	if resolveServer("http://flag", "http://deck") != "http://flag" ||
		resolveServer("", "http://deck") != "http://env" {
		t.Fatal("precedence")
	}
	t.Setenv("AGENT_DECK_SERVER", "")
	if resolveServer("", "http://deck") != "http://deck" || resolveServer("", "") != "http://127.0.0.1:4096" {
		t.Fatal("defaults")
	}
}

func TestNewSessionCLI(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "prompt.txt")
	if e := os.WriteFile(file, []byte("Test prompt"), 0o600); e != nil {
		t.Fatal(e)
	}
	created, prompted := false, false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Query().Get("directory") != dir {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
		switch r.URL.Path {
		case "/session":
			created = true
			fmt.Fprint(w, `{"id":"session-own"}`)
		case "/session/session-own/prompt_async":
			var v struct{ Parts []struct{ Text string } }
			if e := json.NewDecoder(r.Body).Decode(&v); e != nil || len(v.Parts) != 1 ||
				v.Parts[0].Text != "Test prompt" {
				t.Error("invalid prompt")
			}
			prompted = true
			w.WriteHeader(204)
		default:
			t.Error("unexpected endpoint")
			w.WriteHeader(404)
		}
	}))
	defer s.Close()
	if e := run([]string{"new", "--server", s.URL, "--dir", dir, "--title", "Own test", "--prompt-file", file}); e != nil {
		t.Fatal(e)
	}
	if !created || !prompted {
		t.Fatal("missing request")
	}
}

func TestSnapshotTeamOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AGENT_DECK_SERVER", "")
	var paths []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.Query().Get("directory"))
		switch r.URL.Path {
		case "/session/status":
			fmt.Fprint(w, `{}`)
		case "/session/session-team/message":
			fmt.Fprint(w, `[]`)
		default:
			t.Errorf("unexpected %s", r.URL)
			fmt.Fprint(w, `[]`)
		}
	}))
	defer s.Close()
	saved := team.Team{
		Version: 1, Name: "Snap", Repo: "/tmp/snap", Base: "main", Server: s.URL, Capacity: 1,
		Members: []team.Member{{
			Name: "Lead", Role: "master", Mode: "shared", Dir: "/tmp/snap", Session: "session-team",
			Permissions: team.Preset("coder"),
		}},
	}
	if _, err := (team.Store{Root: team.DefaultRoot()}).Save(t.Context(), &saved, nil); err != nil {
		t.Fatal(err)
	}
	if err := dashboard([]string{"--team", "Snap", "--snapshot"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"/session/status?/tmp/snap", "/session/session-team/message?/tmp/snap"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("requests=%v", paths)
	}
}
