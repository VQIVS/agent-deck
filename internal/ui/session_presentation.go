package ui

import "github.com/vqivs/agent-deck/internal/transcript"

func transcriptAnchor(ranges []transcript.Range, offset int) (id string, line int) {
	for _, r := range ranges {
		if offset >= r.Start && offset < r.End {
			return r.ID, offset - r.Start
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
