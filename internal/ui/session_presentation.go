package ui

import (
	"fmt"

	"github.com/vqivs/agent-deck/internal/transcript"
)

func (p *pane) historyNotice() string {
	if p.History {
		return fmt.Sprintf(" · HISTORY / loaded only · +%d new · G Latest", p.NewParts)
	}
	return ""
}

func (p *pane) observeHistoryParts() {
	if p.History && len(p.State.Parts) > p.HistoryParts {
		p.NewParts += len(p.State.Parts) - p.HistoryParts
	}
	p.HistoryParts = len(p.State.Parts)
}

func transcriptAnchor(ranges []transcript.Range, offset int) (id string, line int) {
	var nearest transcript.Range
	for _, r := range ranges {
		if r.ID == "" {
			continue
		}
		if r.Start <= offset {
			nearest = r
		}
		if offset >= r.Start && offset < r.End {
			return r.ID, offset - r.Start
		}
	}
	if nearest.ID != "" {
		return nearest.ID, max(0, offset-nearest.Start)
	}
	for _, r := range ranges {
		if r.ID != "" {
			return r.ID, 0
		}
	}
	return "", 0
}

func (m *Model) historyNavigation(key string) bool {
	if len(m.panes) == 0 {
		return false
	}
	p := &m.panes[m.focus]
	switch key {
	case "f":
		p.History = !p.History
	case "G", "end":
		p.History = false
		p.NewParts = 0
		p.View.GotoBottom()
	default:
		return false
	}
	return true
}

func transcriptAnchorOffset(ranges []transcript.Range, id string, line int) (int, bool) {
	for _, r := range ranges {
		if r.ID == id && id != "" {
			return r.Start + min(line, max(0, r.End-r.Start-1)), true
		}
	}
	return 0, false
}
