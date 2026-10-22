package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
