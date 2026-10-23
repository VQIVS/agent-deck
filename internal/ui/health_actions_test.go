package ui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qivs/agent-deck/internal/team"
)

func TestCompactUsesSessionModelAndRefusesBusyOrWaiting(t *testing.T) {
	busy, waiting, compacted := false, false, 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != "/tmp/team" {
			t.Error("unscoped compact")
		}
		switch r.URL.Path {
		case "/permission":
			if waiting {
				fmt.Fprint(w, `[{"sessionID":"session-a"}]`)
			} else {
				fmt.Fprint(w, `[]`)
			}
		case "/question":
			fmt.Fprint(w, `[]`)
		case "/session/status":
			if busy {
				fmt.Fprint(w, `{"session-a":{"type":"busy"}}`)
			} else {
				fmt.Fprint(w, `{}`)
			}
		case "/session/session-a/message":
			fmt.Fprint(w, `[{"info":{"role":"assistant","providerID":"provider","modelID":"model"}}]`)
		case "/session/session-a/summarize":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["providerID"] != "provider" || body["modelID"] != "model" {
				t.Error(body)
			}
			compacted++
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	m := requestModel(s.URL, false)
	if err := m.compactSession("/tmp/team", "session-a"); err != nil {
		t.Fatal(err)
	}
	busy = true
	if err := m.compactSession("/tmp/team", "session-a"); err == nil {
		t.Fatal("busy compact")
	}
	busy, waiting = false, true
	if err := m.compactSession("/tmp/team", "session-a"); err == nil {
		t.Fatal("waiting compact")
	}
	if compacted != 1 {
		t.Fatal(compacted)
	}
	m.opts.ReadOnly = true
	if m.compact() != nil {
		t.Fatal("read-only compact")
	}
}

func TestAutoNudgePersistsCountersAndDoneStops(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/session/status":
			fmt.Fprint(w, `{}`)
		case "/session":
			fmt.Fprint(w, `[{"id":"session-a"}]`)
		case "/session/session-a/prompt_async":
			w.WriteHeader(http.StatusNoContent)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
	defer s.Close()
	m := requestModel(s.URL, false)
	tm := team.Team{
		Version: 1, Name: "nudge", Repo: "/tmp/team", Base: "main", Server: s.URL, Capacity: 1,
		Members: []team.Member{{
			Name: "Master", Role: "master", Dir: "/tmp/team", Mode: "adopt", Session: "session-a", Capability: "inherited",
			Nudge: team.NudgePolicy{Enabled: true, MaxNudges: 2, MinInterval: time.Second, Message: "continue"},
		}},
	}
	m.OpenTeam(&tm, nil, t.TempDir())
	if err := m.teams.Manager.Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	m.panes[0].State.SetStatus("idle", time.Now().Add(-time.Minute))
	cmds := m.healthTick()
	if len(cmds) != 1 {
		t.Fatal("no automatic nudge")
	}
	m.Update(cmds[0]())
	loaded, _, err := m.teams.Store.Load(t.Context(), "nudge")
	if err != nil || loaded.Members[0].NudgeStatus.Count != 1 {
		t.Fatal(loaded, err)
	}
	m.teams.Manager.Team.Members[0].NudgeStatus.Done = true
	if len(m.healthTick()) != 0 {
		t.Fatal("nudged done member")
	}
}
