package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// SummaryState holds timeline navigation and an exact member filter.
type SummaryState struct {
	Member string
	Offset int
	firstG bool
}

func summaryLines(source *SummarySource, state *SummaryState, width int) []string {
	lines := []string{"Evidence timeline · persisted board history/channel + bounded session excerpts"}
	if source == nil {
		source = &SummarySource{}
		lines = append(lines, "No evidence loaded")
	}
	lines = append(lines, summaryBoardLines(source, state.Member)...)
	lines = append(lines, summaryCoverageLines(source, state.Member)...)
	lines = append(lines,
		"History coverage is limited to supplied persisted records; not a reconstructed full archive.", "")
	for _, entry := range ProjectSummary(source) {
		if state.Member == "" || entry.Member == state.Member {
			lines = append(lines, summaryEntryLines(&entry)...)
		}
	}
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, strings.Split(ansi.Hardwrap(inboxLine(line), max(1, width), true), "\n")...)
	}
	return wrapped
}

func summaryBoardLines(source *SummarySource, member string) []string {
	var lines []string
	if source.Board != nil {
		lines = append(lines, "Current board (state, not inferred from assistant text):")
		for i := range source.Board.Tasks {
			task := &source.Board.Tasks[i]
			if member == "" || kanbanAssignee(task) == member {
				lines = append(lines, fmt.Sprintf("%s [%s] @%s %s",
					task.ID, task.State, kanbanOwner(kanbanAssignee(task)), task.Title))
			}
		}
	}
	return lines
}

func summaryCoverageLines(source *SummarySource, member string) []string {
	lines := make([]string, 0, 2*len(source.Assistants))
	for i := range source.Assistants {
		a := &source.Assistants[i]
		if member != "" && member != a.Member {
			continue
		}
		limit := a.Limit
		if limit <= 0 {
			limit = 30
		}
		lines = append(lines, fmt.Sprintf("Coverage @%s: %d messages loaded; API latest %d only;",
			a.Member, len(a.Messages), limit),
			"earlier transcript may be missing. Assistant text capped at 4000 characters per excerpt.")
	}
	return lines
}

func summaryEntryLines(entry *SummaryEntry) []string {
	label := "persisted"
	if entry.Kind == "report" {
		label = "structured worker report · claims unverified"
	}
	if entry.Inferred {
		label = "inferred/unverified excerpt"
	}
	at := "time unknown"
	if !entry.At.IsZero() {
		at = entry.At.UTC().Format("2006-01-02 15:04")
	}
	lines := make([]string, 0, 2+strings.Count(entry.Text, "\n")+1)
	lines = append(lines, fmt.Sprintf("%s @%s %s [%s] %s",
		at, kanbanOwner(entry.Member), entry.TaskID, label, entry.Kind))
	lines = append(lines, strings.Split(entry.Text, "\n")...)
	lines = append(lines, "")
	return lines
}

func RenderSummary(source *SummarySource, state *SummaryState, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if state == nil {
		state = &SummaryState{}
	}
	lines := summaryLines(source, state, width)
	page := max(0, height-2)
	state.Offset = max(0, min(state.Offset, max(0, len(lines)-page)))
	footer := fmt.Sprintf("j/k · gg/G · PgUp/PgDn · Tab member [%s] · r refresh · Esc back", kanbanOwner(state.Member))
	view := make([]string, 0, page+2)
	view = append(view, RenderInboxLine("Summaries · evidence from available records", width))
	view = append(view, lines[state.Offset:min(len(lines), state.Offset+page)]...)
	view = append(view, RenderInboxLine(footer, width))
	return strings.Join(view[:min(len(view), height)], "\n")
}

func SummaryKey(source *SummarySource, state *SummaryState, key tea.KeyMsg, pageHeight int) bool {
	if state == nil {
		return key.String() == "esc"
	}
	k := key.String()
	if k != "g" {
		state.firstG = false
	}
	switch k {
	case "esc":
		return true
	case "j", "down":
		if state.Offset < int(^uint(0)>>1) {
			state.Offset++
		}
	case "k", "up":
		state.Offset = max(0, state.Offset-1)
	case "pgdown", "ctrl+d":
		step := max(1, pageHeight-2)
		state.Offset = min(state.Offset, int(^uint(0)>>1)-step) + step
	case "pgup", "ctrl+u":
		state.Offset = max(0, state.Offset-max(1, pageHeight-2))
	case "g":
		if state.firstG {
			state.Offset = 0
			state.firstG = false
		} else {
			state.firstG = true
		}
	case "G", "end":
		state.Offset = int(^uint(0) >> 1)
	case "home":
		state.Offset = 0
	case "tab":
		state.Member = kanbanNextValue(summaryMembers(source), state.Member)
		state.Offset = 0
	}
	return false
}

func summaryMembers(source *SummarySource) []string {
	if source == nil {
		return nil
	}
	var members []string
	if source.Board != nil {
		for i := range source.Board.Tasks {
			members = append(members, kanbanAssignee(&source.Board.Tasks[i]))
		}
	}
	for _, assistant := range source.Assistants {
		members = append(members, assistant.Member)
	}
	for _, entry := range ProjectSummary(source) {
		members = append(members, entry.Member)
	}
	slices.Sort(members)
	members = slices.Compact(members)
	return slices.DeleteFunc(members, func(member string) bool { return member == "" })
}
