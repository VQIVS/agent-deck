package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	api "github.com/vqivs/agent-deck/internal/opencode"
	"github.com/vqivs/agent-deck/internal/transcript"
)

func TestHistoryAnchorFollowsPartIdentityAcrossReflow(t *testing.T) {
	id, line := transcriptAnchor([]transcript.Range{{ID: "a", Start: 0, End: 5}, {ID: "b", Start: 5, End: 12}}, 7)
	if id != "b" || line != 2 {
		t.Fatal(id, line)
	}
	reflowed := []transcript.Range{{ID: "a", Start: 0, End: 9}, {ID: "b", Start: 9, End: 17}}
	offset, ok := transcriptAnchorOffset(reflowed, id, line)
	if !ok || offset != 11 {
		t.Fatal(offset, ok)
	}
}

func TestHistoryProductionViewRetainsAnchorAcrossResizeAndAppend(t *testing.T) {
	m := twoSessionModel(t, "http://127.0.0.1:1", false)
	m.w, m.h, m.zoom = 80, 24, true
	p := &m.panes[m.focus]
	for i := range 12 {
		p.State.Parts = append(p.State.Parts, api.Part{
			ID: fmt.Sprintf("part-%d", i), Type: "text", Role: "user",
			Text: fmt.Sprintf("turn-%d ", i) + strings.Repeat("wrapped words ", 20),
		})
	}
	m.Update(key("f"))
	m.View()
	if len(p.Ranges) < 4 {
		t.Fatal("production renderer did not expose loaded history ranges")
	}
	anchor := p.Ranges[0]
	for _, r := range p.Ranges {
		if r.ID == "part-3" {
			anchor = r
		}
	}
	p.View.SetYOffset(anchor.Start)
	p.State.Parts = append(p.State.Parts, api.Part{ID: "latest", Type: "text", Role: "user", Text: "newest-visible-text"})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m.View()
	id, _ := transcriptAnchor(p.Ranges, p.View.YOffset)
	if id != anchor.ID {
		t.Fatalf("visible history anchor changed: %q -> %q", anchor.ID, id)
	}
	m.Update(key("G"))
	view := m.View()
	if p.History || !strings.Contains(view, "newest-visible-text") {
		t.Fatal("Latest did not show newest loaded text")
	}
}

func TestTranscriptHistoryDoesNotDependOnZoom(t *testing.T) {
	m := twoSessionModel(t, "http://127.0.0.1:1", false)
	m.paneNavigation("f")
	if !m.panes[m.focus].History || m.zoom {
		t.Fatal("history did not toggle independently")
	}
	m.paneNavigation("G")
	if m.panes[m.focus].History {
		t.Fatal("Latest did not restore recent-turn mode")
	}
}
