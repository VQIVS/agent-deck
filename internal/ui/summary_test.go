package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/channel"
	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

func TestSummaryChronologyDedupAndProvenance(t *testing.T) {
	at := time.Unix(1000, 0)
	change := team.TaskChange{Task: "T-1", Actor: "Sol", At: at, Field: "state", From: "ready", To: "in_progress"}
	source := &SummarySource{Board: &team.TaskBoard{History: []team.TaskChange{change}, Tasks: []team.Task{
		{ID: "T-1", Title: "Fix", State: "blocked", Assignee: "health", History: []team.TaskChange{change}},
	}}, Channel: []channel.Record{{
		ID: "message", Kind: "message", Sender: "health", Text: "Need input", Time: at.Add(time.Minute),
	}}}
	var message api.Message
	message.Info.ID, message.Info.Role = "assistant-1", "assistant"
	message.Info.Time.Created = at.Add(2 * time.Minute).UnixMilli()
	message.Parts = []api.Part{{Type: "text", Text: "I think this is done"}}
	source.Assistants = []SummaryAssistant{{
		Member: "health", Directory: "/repo", Session: "s", Messages: []api.Message{message}, Limit: 30,
	}}
	entries := ProjectSummary(source)
	if len(entries) != 3 || entries[0].Kind != "history" || entries[2].Kind != "assistant" || !entries[2].Inferred {
		t.Fatalf("bad evidence projection: %+v", entries)
	}
	for _, width := range []int{80, 160} {
		view := RenderSummary(source, &SummaryState{}, width, 48)
		for _, text := range []string{
			"Coverage", "30", "unverified", "I think this is done",
		} {
			if !strings.Contains(view, text) {
				t.Fatalf("missing coverage/provenance %q: %s", text, view)
			}
		}
		if lipgloss.Width(view) > width || lipgloss.Height(view) > 48 {
			t.Fatal("unbounded summary")
		}
	}
}

func TestSummaryNavigationAndMemberFilter(t *testing.T) {
	source := &SummarySource{Channel: []channel.Record{
		{ID: "a", Sender: "alice", Text: strings.Repeat("line ", 200)},
		{ID: "b", Sender: "bob", Text: "bob-only"},
	}}
	state := &SummaryState{}
	SummaryKey(source, state, key("G"), 8)
	RenderSummary(source, state, 80, 8)
	if state.SelectedID != "channel:b" {
		t.Fatal("G did not select deterministic unknown-time end")
	}
	SummaryKey(source, state, key("g"), 8)
	SummaryKey(source, state, key("g"), 8)
	if state.SelectedID != "channel:a" {
		t.Fatal("gg did not select oldest")
	}
	SummaryKey(source, state, key("tab"), 8)
	if state.Member != "alice" {
		t.Fatal("member cycling failed")
	}
	view := RenderSummary(source, state, 80, 80)
	if strings.Contains(view, "bob-only") {
		t.Fatal("member filter leaked unrelated entry")
	}
	if !SummaryKey(source, state, key("esc"), 8) {
		t.Fatal("escape did not request close")
	}
}

func TestSummaryCommentsAndCompleteReportClaims(t *testing.T) {
	at := time.Unix(1000, 0)
	source := &SummarySource{Board: &team.TaskBoard{Tasks: []team.Task{{
		ID: "T-2", History: []team.TaskChange{{Actor: "Master", At: at, Field: "state", To: "review"}},
		Comments: []team.TaskComment{{Actor: "reviewer", At: at.Add(time.Second), Text: "Please check"}},
		Completion: &team.ReportRecord{
			Member: "worker", RecordedAt: at.Add(2 * time.Second), Source: "structured",
			Reported: &team.DoneReport{
				Summary: "Finished", Checklist: []team.ReportItem{{ID: "c1", Complete: true, Evidence: "evidence"}},
				Tests:      []team.ReportTest{{CriterionID: "c1", Command: "go test", Outcome: "fail", Details: "timeout"}},
				OpenIssues: []string{"broken"},
			},
		},
	}}}}
	entries := ProjectSummary(source)
	if len(entries) != 3 || entries[0].TaskID != "T-2" ||
		entries[1].Member != "reviewer" || entries[2].Member != "worker" {
		t.Fatalf("lost attribution or chronology: %+v", entries)
	}
	state := &SummaryState{Mode: "detail"}
	view := RenderSummary(source, state, 160, 60)
	for _, want := range []string{
		"c1 complete=true", "go test", "FAIL (reported)", "timeout", "broken", "not independently verified",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q in %s", want, view)
		}
	}
}

func TestSummaryBoundedExcerptsAndEmptyMembers(t *testing.T) {
	messages := make([]api.Message, 3)
	for i := range messages {
		messages[i].Info.ID, messages[i].Info.Role = string(rune('a'+i)), "assistant"
		messages[i].Parts = []api.Part{{Type: "text", Text: strings.Repeat("界", 5000)}}
	}
	source := &SummarySource{Assistants: []SummaryAssistant{
		{Member: "quiet"}, {Member: "worker", Messages: messages, Limit: 2},
	}}
	entries := ProjectSummary(source)
	if len(entries) != 2 || !strings.HasSuffix(entries[0].Text, "[excerpt truncated]") ||
		len([]rune(entries[0].Text)) > 4100 {
		t.Fatalf("unbounded excerpts: count=%d", len(entries))
	}
	state := &SummaryState{}
	SummaryKey(source, state, key("tab"), 8)
	if state.Member != "quiet" {
		t.Fatalf("member without evidence missing from filter: %q", state.Member)
	}
}

func TestSummaryEmptySmallTerminalAndPaging(t *testing.T) {
	for _, source := range []*SummarySource{nil, {}} {
		for _, width := range []int{1, 4, 20, 80} {
			for _, height := range []int{1, 2, 3, 8} {
				state := &SummaryState{}
				for _, command := range []string{"G", "j", "pgdown", "pgup", "k", "tab", "g", "g"} {
					SummaryKey(source, state, key(command), height)
					view := RenderSummary(source, state, width, height)
					if state.Offset < 0 || lipgloss.Width(view) > width || lipgloss.Height(view) > height {
						t.Fatalf("bad bounds %dx%d offset=%d view=%q", width, height, state.Offset, view)
					}
				}
			}
		}
	}
	state := &SummaryState{}
	SummaryKey(nil, state, key("pgdown"), 10)
	if state.Offset != 0 {
		t.Fatal("empty page must retain zero offset")
	}
	SummaryKey(nil, state, key("pgup"), 10)
	if state.Offset != 0 {
		t.Fatal("page up did not restore viewport")
	}
}

func TestSummaryScreenUsesAvailableRecordsAndBack(t *testing.T) {
	m := Model{w: 100, h: 30}
	m.channel.Records = []channel.Record{{ID: "visible", Sender: "alice", Text: "available channel evidence"}}
	m.panes = []pane{{Messages: []api.Message{{Parts: []api.Part{
		{Type: "text", Text: "user text must not become assistant evidence"},
	}}}}}
	source := m.summarySource()
	if len(source.Channel) != 1 || len(source.Assistants) != 1 || source.Assistants[0].Limit != 30 {
		t.Fatalf("bad snapshot adapter: %+v", source)
	}
	view := m.summaryView()
	if !strings.Contains(view, "available channel evidence") || strings.Contains(view, "user text must not") {
		t.Fatalf("wrong evidence in screen: %s", view)
	}
	for _, command := range []string{"esc", "q"} {
		m.workspace.View = "summary"
		m.summaryKey(key(command))
		if m.workspace.View != "dashboard" {
			t.Fatalf("%s did not leave summary", command)
		}
	}
}
