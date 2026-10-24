package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qivs/agent-deck/internal/team"
)

type teamFake struct {
	mu        sync.Mutex
	states    []string
	polls     int
	waiting   bool
	completed bool
	prompts   []map[string]any
	replies   []string
}

func (f *teamFake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	dir := r.URL.Query().Get("directory")
	if dir != "/tmp/worker" && dir != "/tmp/master" {
		http.Error(w, "unscoped directory", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/session/status":
		state := "idle"
		if dir == "/tmp/worker" && len(f.states) > 0 {
			state = f.states[min(f.polls, len(f.states)-1)]
			f.polls++
		}
		fmt.Fprintf(w, `{"worker":{"type":%q}}`, state)
	case "/session":
		fmt.Fprint(w, `[{"id":"worker","time":{"updated":1234}},{"id":"master","time":{"updated":5678}}]`)
	case "/permission":
		if f.waiting && dir == "/tmp/worker" {
			fmt.Fprint(w, `[{"id":"per_worker","sessionID":"worker","permission":"bash","patterns":["go test"]},
{"id":"per_other","sessionID":"unrelated","permission":"edit"}]`)
		} else {
			fmt.Fprint(w, `[]`)
		}
	case "/question":
		if f.waiting && dir == "/tmp/worker" {
			fmt.Fprint(w, `[{"id":"que_worker","sessionID":"worker","questions":[{"header":"Plan","question":"Which?"}]}]`)
		} else {
			fmt.Fprint(w, `[]`)
		}
	case "/session/worker/message", "/session/master/message":
		id, completed := "old", 0
		if f.completed {
			id, completed = "new", 1235
		}
		fmt.Fprintf(w, `[{"info":{"id":%q,"role":"assistant","time":{"created":1234,"completed":%d},
"tokens":{"input":10,"cache":{"read":20}}},"parts":[{"type":"text","text":"hello report"}]}]`, id, completed)
	case "/session/worker/prompt_async":
		var prompt map[string]any
		if err := json.NewDecoder(r.Body).Decode(&prompt); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.prompts = append(f.prompts, prompt)
		w.WriteHeader(http.StatusNoContent)
	case "/permission/per_worker/reply":
		var reply struct{ Reply, Message string }
		_ = json.NewDecoder(r.Body).Decode(&reply)
		f.replies = append(f.replies, reply.Reply+":"+reply.Message+":"+dir)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func setupTeamCLI(t *testing.T, fake *teamFake) func(...string) (string, error) {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(fake.serve))
	t.Cleanup(s.Close)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("AGENT_DECK_SERVER", "")
	tm := team.Team{
		Version: 1, Name: "test", Server: s.URL, Repo: "/tmp/master", Base: "main", Capacity: 2,
		Rules: "shared rules", Members: []team.Member{
			{Name: "Master", Role: "master", Dir: "/tmp/master", Session: "master", Mode: "adopt", Capability: "inherited"},
			{
				Name: "Worker", Role: "worker", Dir: "/tmp/worker", Session: "worker", Mode: "adopt",
				Capability: "inherited", Agent: "build", Model: "provider/model", System: "worker rules",
			},
			{Name: "Unstarted", Role: "worker", Dir: "/tmp/worker", Mode: "shared", Capability: "inherited"},
		},
	}
	if _, err := (team.Store{Root: team.DefaultRoot()}).Save(context.Background(), &tm, nil); err != nil {
		t.Fatal(err)
	}
	return func(args ...string) (string, error) {
		var out bytes.Buffer
		err := teamCommand(context.Background(), args, &out)
		return out.String(), err
	}
}

func TestHeadlessTeamReads(t *testing.T) {
	fake := &teamFake{waiting: true}
	run := setupTeamCLI(t, fake)
	for _, command := range []string{"ls", "status", "requests"} {
		out, err := run(command, "--team", "test", "--json", "--read-only")
		if err != nil || !json.Valid([]byte(out)) {
			t.Fatalf("%s: %s %v", command, out, err)
		}
		if strings.Contains(out, "per_other") {
			t.Fatal("unrelated permission leaked")
		}
	}
	out, err := run("status", "--team", "test", "--json")
	var statuses []team.MemberStatus
	if err != nil || json.Unmarshal([]byte(out), &statuses) != nil {
		t.Fatal(out, err)
	}
	if statuses[1].State != "waiting" || statuses[1].Pending != 2 || statuses[1].ContextTokens != 30 ||
		statuses[1].LastActivity != 1234 || statuses[2].State != "offline" {
		t.Fatal(statuses)
	}
	out, err = run("last", "--team", "test", "--member", "Worker")
	if err != nil || out != "hello report\n" {
		t.Fatal(out, err)
	}
}

func TestHeadlessTeamSendAndReplies(t *testing.T) {
	fake := &teamFake{waiting: true}
	run := setupTeamCLI(t, fake)
	out, err := run("send", "--team", "test", "--member", "Worker", "--text", "hello", "--json")
	if err != nil || !json.Valid([]byte(out)) || len(fake.prompts) != 1 {
		t.Fatal(out, err)
	}
	prompt := fake.prompts[0]
	if prompt["system"] != "shared rules\n\nworker rules" || prompt["agent"] != "build" ||
		prompt["model"].(map[string]any)["modelID"] != "model" {
		t.Fatal(prompt)
	}
	for _, args := range [][]string{
		{"approve", "--id", "per_worker"},
		{"approve", "--id", "per_worker", "--always"},
		{"reject", "--id", "per_worker", "--message", "no"},
	} {
		if _, err := run(append(args, "--team", "test")...); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(fake.replies, ",") != "once::/tmp/worker,always::/tmp/worker,reject:no:/tmp/worker" {
		t.Fatal(fake.replies)
	}
	if _, err := run("approve", "--team", "test", "--id", "per_other"); err == nil {
		t.Fatal("replied to unrelated session")
	}
	if _, err := run("approve", "--team", "test", "--id", "que_worker"); err == nil {
		t.Fatal("question treated as permission")
	}
}

func TestHeadlessReadOnlyAndValidation(t *testing.T) {
	fake := &teamFake{waiting: true}
	run := setupTeamCLI(t, fake)
	for _, args := range [][]string{
		{"send", "--member", "Worker", "--text", "hi", "--read-only"},
		{"approve", "--id", "per_worker", "--read-only"},
		{"reject", "--id", "per_worker", "--read-only"},
		{"send", "--member", "Unstarted", "--text", "hi"},
		{"send", "--member", "missing", "--text", "hi"},
		{"send", "--member", "Worker", "--text", "hi", "--file", "missing"},
		{"wait", "--member", "Worker", "--poll", "0s"},
	} {
		if _, err := run(append(args, "--team", "test")...); err == nil {
			t.Fatal("accepted", args)
		}
	}
	if len(fake.prompts)+len(fake.replies) != 0 {
		t.Fatal("validation caused mutation")
	}
	file := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(file, []byte("from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run("send", "--team", "test", "--member", "Worker", "--file", file); err != nil {
		t.Fatal(err)
	}
}

func TestWaitDelayedStartAndFastCompletion(t *testing.T) {
	fake := &teamFake{states: []string{"idle", "idle", "busy", "idle"}}
	run := setupTeamCLI(t, fake)
	if _, err := run("send", "--team", "test", "--member", "Worker", "--text", "hi"); err != nil {
		t.Fatal(err)
	}
	out, err := run("wait", "--team", "test", "--member", "Worker", "--poll", "1ms", "--timeout", "1s")
	if err != nil || !strings.Contains(out, "idle") || fake.polls < 4 {
		t.Fatal(out, err, fake.polls)
	}
	fake.mu.Lock()
	fake.states, fake.completed = []string{"idle"}, true
	fake.mu.Unlock()
	if _, err := run("wait", "--team", "test", "--member", "Worker", "--poll", "1ms", "--timeout", "1s"); err != nil {
		t.Fatal("missed fast completed response", err)
	}
}

func TestWaitExitCodes(t *testing.T) {
	fake := &teamFake{waiting: true}
	run := setupTeamCLI(t, fake)
	_, err := run("wait", "--team", "test", "--member", "Worker", "--timeout", "1s", "--json")
	assertExitCode(t, err, 2)
	fake.mu.Lock()
	fake.waiting = false
	fake.mu.Unlock()
	if _, err := run("send", "--team", "test", "--member", "Worker", "--text", "hi"); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	_, err = run("wait", "--team", "test", "--member", "Worker", "--timeout", "30ms", "--poll", "1ms")
	assertExitCode(t, err, 3)
	if time.Since(started) < 25*time.Millisecond {
		t.Fatal("send+wait returned before observing a response")
	}
}

func TestReceiptUsesStateDirectory(t *testing.T) {
	run := setupTeamCLI(t, &teamFake{})
	if _, err := run("send", "--team", "test", "--member", "Worker", "--text", "hello"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.Getenv("XDG_STATE_HOME"), "agent-deck", "sent", "test", "worker.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal("missing state receipt", err)
	}
	if _, err := os.Stat(filepath.Join(team.DefaultRoot(), ".sent")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("receipt written into configuration")
	}
}

func TestHeadlessServerOverrideAndOffline(t *testing.T) {
	run := setupTeamCLI(t, &teamFake{})
	t.Setenv("AGENT_DECK_SERVER", "http://127.0.0.1:1")
	_, err := run("last", "--team", "test", "--member", "Worker")
	if err == nil {
		t.Fatal("server environment override ignored")
	}
	good := httptest.NewServer(http.HandlerFunc((&teamFake{}).serve))
	defer good.Close()
	out, err := run("last", "--team", "test", "--member", "Worker", "--server", good.URL)
	if err != nil || out != "hello report\n" {
		t.Fatal("explicit server override ignored", out, err)
	}
	out, err = run("status", "--team", "test", "--json")
	if err != nil || !strings.Contains(out, `"state":"offline"`) || !strings.Contains(out, `"error"`) {
		t.Fatal("offline status not reported", out, err)
	}
}

func assertExitCode(t *testing.T, err error, code int) {
	t.Helper()
	var exit *exitError
	if !errors.As(err, &exit) || exit.Code != code {
		t.Fatalf("want exit %d, got %v", code, err)
	}
}
