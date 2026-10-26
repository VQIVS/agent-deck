package ui

import (
	"testing"

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
