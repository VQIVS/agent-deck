package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/vqivs/agent-deck/internal/channel"
	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/team"
)

func summaryDesignFixture() *SummarySource {
	at := time.Date(2026, 10, 25, 13, 40, 0, 0, time.UTC)
	loaded := SummaryStatus{Loaded: true, LastSuccess: at}
	source := &SummarySource{
		Team: "agent-deck", Members: []string{"lead", "core-dev", "ui-dev", "quiet"},
		BoardStatus: loaded, ChannelStatus: loaded,
		Board: &team.TaskBoard{Tasks: []team.Task{
			{ID: "T-31", Title: "Git TUI research", State: "review", Assignee: "core-dev"},
			{ID: "T-34", Title: "Summary discovery", State: "in_progress", Assignee: "ui-design"},
			{ID: "T-32", Title: "Waiting for scope", State: "blocked", Assignee: "ui-dev"},
		}},
	}
	source.Board.History = []team.TaskChange{{
		Task: "T-34", Actor: "lead", At: at.Add(-9 * time.Minute), Field: "state", From: "ready", To: "in_progress",
	}}
	source.Board.Tasks[0].Comments = []team.TaskComment{{
		Actor: "qa", At: at.Add(-6 * time.Minute), Text: "Please preserve no-tool fallback",
	}}
	source.Channel = []channel.Record{{
		ID: "scope", Sender: "ui-dev", Time: at.Add(-5 * time.Minute), Text: "Need a scope decision before proceeding",
	}}
	source.Board.Tasks[0].Completion = &team.ReportRecord{
		Member: "core-dev", Source: "structured", RecordedAt: at, Reported: &team.DoneReport{
			Summary:   "Done",
			Checklist: []team.ReportItem{{ID: "c1", Complete: true, Evidence: "Compared upstream documentation"}},
			Tests: []team.ReportTest{{
				CriterionID: "c1", Command: "go test ./internal/ui", Outcome: "fail", Details: "timeout",
			}},
			OpenIssues: []string{"no-tool fallback needs review"},
		},
	}
	return source
}

// Run with -v to supply reproducible full-terminal captures to QA/designer.
func TestSummaryDesignCaptures(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {160, 48}} {
		variants := []string{
			"loaded", "loading", "empty", "filter", "stale", "error", "coverage", "detail", "picker",
			"refresh", "channel-error", "no-board-cache", "new", "long-unicode", "help", "session-coverage",
		}
		for _, variant := range variants {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], variant), func(t *testing.T) {
				source := summaryDesignFixture()
				state := &SummaryState{}
				switch variant {
				case "loading":
					source = &SummarySource{
						Team: "agent-deck", BoardStatus: SummaryStatus{Loading: true}, ChannelStatus: SummaryStatus{Loading: true},
					}
				case "empty":
					source.Board = &team.TaskBoard{}
					source.Channel = nil
				case "filter":
					state.Member = "quiet"
				case "stale":
					source.BoardStatus.Error = "permission denied"
				case "refresh":
					source.BoardStatus.Loading = true
					source.ChannelStatus.Loading = true
				case "channel-error":
					source.ChannelStatus.Error = "channel read failed"
				case "no-board-cache":
					source.BoardStatus = SummaryStatus{Error: "permission denied"}
					source.Board = nil
				case "new":
					state.sync(source)
					state.move(-1)
					source.Channel = append(source.Channel, channel.Record{
						ID: "new", Sender: "lead", Text: "Arrived while reading",
						Time: time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC),
					})
				case "long-unicode":
					source.Board.Tasks[0].Title = strings.Repeat("界é", 100)
					state.Mode = "detail"
				case "session-coverage":
					source.Assistants = summaryCoverageFixture()
					state.Mode = "coverage"
				case "error":
					source = &SummarySource{
						BoardStatus: SummaryStatus{Error: "permission denied"}, ChannelStatus: SummaryStatus{Error: "read failed"},
					}
				case "coverage", "detail", "picker", "help":
					state.Mode = variant
				}
				view := "1 Dashboard  2 Git  [3 Summary]  4 Channel  5 Board  6 Inbox\n" +
					RenderSummary(source, state, size[0], size[1]-1)
				if variant == "session-coverage" {
					state.DetailOffset = 1 << 25
					t.Log("Scrolled session coverage:\n" + RenderSummary(source, state, size[0], size[1]-1))
				}
				if lipgloss.Height(view) != size[1] || lipgloss.Width(view) > size[0] {
					t.Fatalf("bad full-terminal bounds: %dx%d", lipgloss.Width(view), lipgloss.Height(view))
				}
				rows := strings.Split(view, "\n")
				for i, row := range rows[1:] {
					if lipgloss.Width(row) != size[0] {
						t.Fatalf("row%d width=%d", i+2, lipgloss.Width(row))
					}
				}
				if !strings.Contains(view, "LIMITED") {
					t.Fatal("missing persistent coverage")
				}
				if variant == "loaded" && (!strings.Contains(view, "Evidence / oldest") ||
					!strings.Contains(view, "REPORT / worker claim")) {
					t.Fatal("missing structured evidence/authority")
				}
				if variant == "stale" && !strings.Contains(view, "STALE") {
					t.Fatal("cache freshness missing")
				}
				if variant == "loading" && strings.Contains(view, "0 tasks") {
					t.Fatal("loading fabricated empty board")
				}
				if variant == "loading" && strings.Contains(view, "No matching") {
					t.Fatal("loading implied loaded-empty")
				}
				if variant == "new" && !strings.Contains(view, "+1 new; G newest") {
					t.Fatal("new records notice clipped")
				}
				if variant == "loaded" && size[0] == 160 && !strings.Contains(view, "Current board: review") {
					t.Fatal("report preview lost adjacent board truth")
				}
				t.Log("\n" + view)
			})
		}
	}
}

func summaryCoverageFixture() []SummaryAssistant {
	var message api.Message
	message.Info.ID, message.Info.Role = "excerpt", "assistant"
	message.Info.Time.Created = time.Date(2026, 10, 25, 13, 38, 0, 0, time.UTC).UnixMilli()
	message.Parts = []api.Part{{Type: "text", Text: strings.Repeat("界", 4100)}}
	loaded := SummaryStatus{Loaded: true, LastSuccess: time.Date(2026, 10, 25, 13, 40, 0, 0, time.UTC)}
	return []SummaryAssistant{
		{Member: "quiet", Directory: "/repo", Session: "quiet-session", Limit: 30, Status: loaded},
		{Member: "unbound", Directory: "/repo", Limit: 30},
		{
			Member: "failed", Directory: "/repo", Session: "failed-session", Limit: 30,
			Status: SummaryStatus{Error: "fetch denied"},
		},
		{
			Member: "worker", Directory: "/repo/work", Session: "worker-session", Limit: 30,
			Status: loaded, Messages: []api.Message{message},
		},
	}
}

func TestSummaryAnchorAndScope(t *testing.T) {
	source := summaryDesignFixture()
	s := &SummaryState{}
	RenderSummary(source, s, 80, 23)
	SummaryKey(source, s, key("k"), 23)
	anchor := s.SelectedID
	source.Channel = append(source.Channel, channel.Record{
		ID: "new", Sender: "lead", Time: time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC), Text: "New record",
	})
	RenderSummary(source, s, 160, 47)
	if s.SelectedID != anchor || s.New != 1 {
		t.Fatalf("arrival stole anchor: %+v", s)
	}
	SummaryKey(source, s, key("G"), 47)
	if s.SelectedID != "channel:new" || s.New != 0 {
		t.Fatal("G did not explicitly follow newest")
	}
	s.filter(source, "quiet")
	if len(s.entries) != 0 || s.SelectedID != "" {
		t.Fatal("quiet filter leaked authored evidence")
	}
	SummaryKey(source, s, key("a"), 47)
	if s.Member != "" || s.SelectedID != "channel:new" {
		t.Fatal("All did not reset scope/newest")
	}
}

func TestSummaryFetchMetadataKeepsCache(t *testing.T) {
	loaded := SummaryStatus{}
	summaryFetchResult(&loaded, nil)
	last := loaded.LastSuccess
	summaryFetchResult(&loaded, errors.New("permission denied"))
	if !loaded.Loaded || loaded.LastSuccess != last || loaded.Error == "" {
		t.Fatal("failure erased successful cache identity")
	}
	source := summaryDesignFixture()
	source.BoardStatus = loaded
	s := &SummaryState{}
	view := RenderSummary(source, s, 80, 23)
	if !strings.Contains(view, "STALE") || !strings.Contains(view, "REPORT") {
		t.Fatal("source failure erased healthy evidence")
	}
}

func TestSummaryResponsiveUnicodeBounds(t *testing.T) {
	source := summaryDesignFixture()
	source.Channel[0].Sender = strings.Repeat("界é", 80)
	source.Channel[0].Text = strings.Repeat("界\x1b[31munsafe\x07 é ", 100)
	for _, size := range [][2]int{{160, 47}, {120, 39}, {80, 23}, {60, 17}, {40, 11}, {39, 10}, {1, 1}, {0, 0}} {
		for _, mode := range []string{"", "detail", "coverage", "picker", "help"} {
			view := RenderSummary(source, &SummaryState{Mode: mode}, size[0], size[1])
			if size[0] == 0 || size[1] == 0 {
				if view != "" {
					t.Fatal("zero dimension must render empty")
				}
				continue
			}
			if lipgloss.Width(view) > size[0] || lipgloss.Height(view) > size[1] {
				t.Fatalf("overflow %v mode=%s", size, mode)
			}
		}
	}
}

func TestSummaryPickerInputAndCancel(t *testing.T) {
	source := summaryDesignFixture()
	s := &SummaryState{Member: "core-dev"}
	SummaryKey(source, s, key("m"), 23)
	for _, text := range []string{"q", "u", "i", "e", "t"} {
		SummaryKey(source, s, key(text), 23)
	}
	if s.pickerSearch != "quiet" || s.Member != "core-dev" {
		t.Fatal("typing changed scope before apply")
	}
	SummaryKey(source, s, key("esc"), 23)
	if s.Member != "core-dev" || s.Mode != "" {
		t.Fatal("cancel did not preserve exact scope")
	}
	SummaryKey(source, s, key("m"), 23)
	for _, text := range []string{"q", "u", "i", "e", "t"} {
		SummaryKey(source, s, key(text), 23)
	}
	SummaryKey(source, s, key("enter"), 23)
	if s.Member != "quiet" || s.Mode != "" {
		t.Fatalf("apply failed: %+v", s)
	}
	SummaryKey(source, s, key("c"), 23)
	SummaryKey(source, s, key("j"), 23)
	if s.DetailOffset != 1 || s.Mode != "coverage" {
		t.Fatal("coverage did not own scrolling")
	}
	SummaryKey(source, s, key("esc"), 23)
	if s.Mode != "" {
		t.Fatal("Esc did not return list")
	}
}

func TestSummaryBoardBoundedAndUnknownTimes(t *testing.T) {
	source := summaryDesignFixture()
	for i := 0; i < 110; i++ {
		source.Board.Tasks = append(source.Board.Tasks, team.Task{
			ID: fmt.Sprintf("T-%d", i+100), State: "custom-review", Title: "Large board", Assignee: "quiet",
		})
	}
	source.Channel = append(source.Channel, channel.Record{
		ID: "unknown", Sender: "departed", Text: "No timestamp supplied",
	})
	s := &SummaryState{}
	view := RenderSummary(source, s, 80, 23)
	if !strings.Contains(view, "more tasks") || !strings.Contains(view, "custom-review") ||
		!strings.Contains(view, "Evidence /") {
		t.Fatal("large board displaced evidence or hid custom state")
	}
	entries := ProjectSummary(source)
	if entries[len(entries)-1].ID != "channel:unknown" || !entries[len(entries)-1].At.IsZero() {
		t.Fatal("unknown timestamp invented or sorted into dated group")
	}
	if s.SelectedID == "channel:unknown" {
		t.Fatal("initial selection should prefer newest known timestamp")
	}
	SummaryKey(source, s, key("down"), 23)
	if s.SelectedID != "channel:unknown" {
		t.Fatal("unknown-time group unreachable")
	}
}

func TestSummaryMouseSelectionDetailsAndResize(t *testing.T) {
	m := Model{w: 80, h: 23}
	source := summaryDesignFixture()
	m.board.State.Board = *source.Board
	m.channel.Records = source.Channel
	m.summary.boardStatus = source.BoardStatus
	m.summary.channelStatus = source.ChannelStatus
	m.workspace.View = "summary"
	m.summaryView()
	var event, details zone
	for _, z := range m.summary.zones {
		if strings.HasPrefix(z.Action, "summary:event:") && event.Action == "" {
			event = z
		}
		if z.Action == "summary:detail" {
			details = z
		}
	}
	if event.Action == "" || details.Action == "" {
		t.Fatal("missing visible event/detail hit targets")
	}
	if handled, _ := m.summaryClick(event, true); !handled || m.summary.Mode != "" {
		t.Fatal("event click opened instead of selecting only")
	}
	if handled, _ := m.summaryClick(details, true); !handled || m.summary.Mode != "detail" {
		t.Fatal("Details action did not open selected item")
	}
	selected := m.summary.SelectedID
	m.summaryWheel(3)
	if m.summary.DetailOffset != 3 || m.summary.SelectedID != selected {
		t.Fatal("detail wheel changed selected evidence")
	}
	m.w, m.h = 39, 10
	m.summaryView()
	if len(m.summary.zones) != 0 {
		t.Fatal("tiny resize retained stale hit zones")
	}
}

func TestSummaryProductionShellCaptures(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {160, 48}} {
		for _, name := range []string{"dark", "light", "monochrome"} {
			t.Run(fmt.Sprintf("%dx%d/%s", size[0], size[1], name), func(t *testing.T) {
				source := summaryDesignFixture()
				m := Model{w: size[0], h: size[1]}
				m.teams = &teamUI{Manager: &team.Manager{Team: team.Team{Name: source.Team}}}
				if name != "monochrome" {
					m.theme, _ = ResolveTheme(name)
				}
				m.workspace.View = "summary"
				m.board.State.Board = *source.Board
				m.channel.Records = source.Channel
				m.summary.boardStatus = source.BoardStatus
				m.summary.channelStatus = source.ChannelStatus
				view := m.workspaceView()
				if lipgloss.Width(view) > size[0] || lipgloss.Height(view) != size[1] {
					t.Fatalf("production shell bounds %dx%d", lipgloss.Width(view), lipgloss.Height(view))
				}
				if !strings.Contains(view, "Summary") || !strings.Contains(view, "Coverage") {
					t.Fatal("production shell lost Summary sections")
				}
				if name == "monochrome" {
					assertScreenGolden(t, fmt.Sprintf("summary_%dx%d", size[0], size[1]), view)
				}
				t.Log("\n" + view)
			})
		}
	}
}

func TestSummaryWholeEventsAndCoverageContext(t *testing.T) {
	source := summaryDesignFixture()
	s := &SummaryState{}
	s.sync(source)
	s.move(-1)
	source.Channel = append(source.Channel, channel.Record{
		ID: "arrival", Sender: "lead", Time: time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC), Text: "Newest preview",
	})
	view := RenderSummary(source, s, 80, 23)
	if strings.Contains(view, "14:00") && !strings.Contains(view, "Newest preview") {
		t.Fatal("compact viewport displayed orphan event metadata")
	}
	source.Assistants = summaryCoverageFixture()
	s.Mode = "coverage"
	s.DetailOffset = 1 << 25
	view = RenderSummary(source, s, 80, 23)
	if !strings.Contains(strings.Split(view, "\n")[8], "worker") {
		t.Fatal("scrolled coverage lost pinned member identity")
	}
}

func TestSummaryObserverAndRegionWheel(t *testing.T) {
	m := Model{w: 80, h: 23}
	m.summaryObserve(boardResult{})
	if !m.summary.boardStatus.Loaded {
		t.Fatal("successful empty board fetch not recorded")
	}
	last := m.summary.boardStatus.LastSuccess
	m.summaryObserve(boardResult{Err: errors.New("refresh failed")})
	if !m.summary.boardStatus.Loaded || m.summary.boardStatus.LastSuccess != last {
		t.Fatal("observer lost cached board freshness")
	}
	m.summaryObserve(channelResult{})
	if !m.summary.channelStatus.Loaded {
		t.Fatal("successful empty channel fetch not recorded")
	}
	source := summaryDesignFixture()
	m.board.State.Board = *source.Board
	m.channel.Records = source.Channel
	m.summary.boardStatus = source.BoardStatus
	m.summary.Mode = "coverage"
	m.summaryView()
	m.summaryWheelAt(10, 2, 3)
	if m.summary.DetailOffset != 0 {
		t.Fatal("header wheel changed reading position")
	}
	m.summaryWheelAt(10, m.summary.contentTop+2, 3)
	if m.summary.DetailOffset != 3 {
		t.Fatal("content wheel did not scroll within replacement region")
	}
}
