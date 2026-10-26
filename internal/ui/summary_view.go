package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// SummaryState stores stable evidence identity separately from rendered offsets.
type SummaryState struct {
	Member, SelectedID, Mode   string
	Offset, DetailOffset       int
	New                        int
	Notice                     string
	firstG                     bool
	seen                       map[string]bool
	entries                    []SummaryEntry
	width, height, page        int
	pickerSearch               string
	pickerIndex, pickerFocus   int
	pickerCursor               int
	boardStatus, channelStatus SummaryStatus
	assistantStatus            map[string]SummaryStatus
	zones                      []zone
	selectedRows               map[int]bool
	contentTop, contentBottom  int
}

func summaryMembers(source *SummarySource) []string {
	if source == nil {
		return nil
	}
	members := slices.Clone(source.Members)
	if source.Board != nil {
		for i := range source.Board.Tasks {
			members = append(members, kanbanAssignee(&source.Board.Tasks[i]))
		}
	}
	for i := range source.Assistants {
		members = append(members, source.Assistants[i].Member)
	}
	for _, e := range ProjectSummary(source) {
		members = append(members, e.Member)
	}
	slices.Sort(members)
	return slices.DeleteFunc(slices.Compact(members), func(s string) bool { return s == "" })
}

func summaryEntries(source *SummarySource, member string) []SummaryEntry {
	return slices.DeleteFunc(ProjectSummary(source), func(e SummaryEntry) bool {
		return member != "" && member != e.Member
	})
}

func (s *SummaryState) sync(source *SummarySource) {
	entries := summaryEntries(source, s.Member)
	previous := s.entries
	if s.seen != nil {
		for _, e := range entries {
			if !s.seen[e.ID] {
				s.New++
			}
		}
	}
	s.seen = map[string]bool{}
	for _, e := range entries {
		s.seen[e.ID] = true
	}
	s.entries = entries
	if len(entries) == 0 {
		s.SelectedID = ""
		s.Offset = 0
		return
	}
	if slices.ContainsFunc(entries, func(e SummaryEntry) bool { return e.ID == s.SelectedID }) {
		return
	}
	if s.SelectedID != "" {
		old := slices.IndexFunc(previous, func(e SummaryEntry) bool { return e.ID == s.SelectedID })
		if old >= 0 {
			idx := slices.IndexFunc(entries, func(e SummaryEntry) bool { return !e.At.Before(previous[old].At) })
			if idx < 0 {
				idx = len(entries) - 1
			}
			s.SelectedID = entries[idx].ID
			s.Notice = "Selected record disappeared; nearest loaded record selected"
			return
		}
	}
	s.selectNewest()
}

func (s *SummaryState) selected() int {
	return slices.IndexFunc(s.entries, func(e SummaryEntry) bool { return e.ID == s.SelectedID })
}

func (s *SummaryState) selectNewest() {
	idx := len(s.entries) - 1
	for i := len(s.entries) - 1; i >= 0; i-- {
		if !s.entries[i].At.IsZero() {
			idx = i
			break
		}
	}
	if idx >= 0 {
		s.SelectedID = s.entries[idx].ID
	}
	s.New = 0
}

func (s *SummaryState) filter(source *SummarySource, member string) {
	s.Member, s.SelectedID, s.Mode = member, "", ""
	s.Offset, s.DetailOffset, s.New = 0, 0, 0
	s.seen = nil
	s.sync(source)
}

func summaryAuthority(e *SummaryEntry) (kind, authority string) {
	if e.Inferred {
		return "EXCERPT", "unverified"
	}
	switch e.Kind {
	case "report":
		return "REPORT", "worker claim"
	case "history":
		return "BOARD", "recorded"
	case "comment":
		return "COMMENT", "recorded"
	default:
		return "CHANNEL", "recorded"
	}
}

func summaryTime(at time.Time) string {
	if at.IsZero() {
		return "unknown"
	}
	return at.UTC().Format("2006-01-02 15:04:05 UTC")
}

func summaryStatusText(name string, status SummaryStatus) string {
	if status.Loading {
		if status.Loaded {
			return name + " refreshing"
		}
		return name + " loading"
	}
	if status.Error != "" {
		if status.Loaded {
			when := "time unknown"
			if !status.LastSuccess.IsZero() {
				when = status.LastSuccess.UTC().Format("15:04 UTC")
			}
			return name + " STALE " + when
		}
		return name + " unavailable"
	}
	if status.Loaded {
		when := "time unknown"
		if !status.LastSuccess.IsZero() {
			when = status.LastSuccess.UTC().Format("15:04 UTC")
		}
		return name + " loaded " + when
	}
	return name + " not loaded"
}

func summaryCell(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = strings.ReplaceAll(inboxLine(text), "\n", " ")
	text = ansi.Truncate(text, width, "...")
	return text + strings.Repeat(" ", max(0, width-ansi.StringWidth(text)))
}

type summaryCanvas struct {
	width int
	rows  []string
	state *SummaryState
	theme *Theme
}

func (c *summaryCanvas) line(y int, text string) {
	if y >= 0 && y < len(c.rows) {
		c.rows[y] = summaryCell(text, c.width)
	}
}

func (c *summaryCanvas) hit(x, y, width int, action string) {
	if width > 0 && y >= 0 && y < len(c.rows) {
		c.state.zones = append(c.state.zones, zone{
			Rect: Rect{X: x, Y: y, W: min(width, c.width-x), H: 1}, Action: "summary:" + action,
		})
	}
}

func (c *summaryCanvas) panel(y, height int, title string) {
	if height < 2 {
		return
	}
	interior := max(0, c.width-2)
	head := ansi.Truncate("- "+title+" ", interior, "...")
	head += strings.Repeat("-", max(0, interior-ansi.StringWidth(head)))
	c.line(y, "+"+head+"+")
	for i := 1; i < height-1; i++ {
		c.line(y+i, "|"+strings.Repeat(" ", interior)+"|")
	}
	c.line(y+height-1, "+"+strings.Repeat("-", interior)+"+")
}

func (c *summaryCanvas) body(y int, text string) {
	c.line(y, "| "+summaryCell(text, max(0, c.width-4))+" |")
}

// renderSummary renders the body below the persistent workspace tab row.
func renderSummary(source *SummarySource, state *SummaryState, width, height int, theme *Theme) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	if source == nil {
		source = &SummarySource{}
	}
	if state == nil {
		state = &SummaryState{}
	}
	state.width, state.height = width, height
	state.zones = nil
	state.selectedRows = map[int]bool{}
	state.contentTop, state.contentBottom = 0, 0
	state.sync(source)
	c := &summaryCanvas{width: width, rows: make([]string, height), state: state, theme: theme}
	for i := range c.rows {
		c.rows[i] = strings.Repeat(" ", width)
	}
	if width < 40 || height < 11 {
		c.line(0, "Summary needs 40x12; resize")
		if height > 1 {
			c.line(height-1, "Esc back")
		}
		return strings.Join(c.rows, "\n")
	}
	c.heading(source)
	c.regions(source)
	c.footer(source)
	if theme != nil {
		c.style()
	}
	return strings.Join(c.rows, "\n")
}

func (c *summaryCanvas) heading(source *SummarySource) {
	state, width := c.state, c.width
	member := state.Member
	if member == "" {
		member = "All members"
	}
	teamName := source.Team
	if teamName == "" {
		teamName = "available records"
	}
	freshness := summaryStatusText("Board", source.BoardStatus) + " / " +
		summaryStatusText("Channel", source.ChannelStatus)
	c.line(0, "Summary / "+teamName+" | "+freshness)
	chip := "Member: [" + member + " v]"
	c.line(1, chip+"  Tab next / m choose | Board: assignee / Events: author")
	c.hit(0, 1, ansi.StringWidth(chip), "picker")
	if width >= 120 {
		action := "[r Refresh board/channel]"
		if source.BoardStatus.Loading || source.ChannelStatus.Loading {
			action = "[refreshing board/channel]"
		}
		x := width - len(action)
		c.rows[0] = ansi.Truncate(c.rows[0], x, "") + action
		if !source.BoardStatus.Loading && !source.ChannelStatus.Loading {
			c.hit(x, 0, len(action), "refresh")
		}
	}
}

func (c *summaryCanvas) regions(source *SummarySource) {
	state, width, height := c.state, c.width, len(c.rows)
	wide := width >= 120 && height >= 39
	if width < 80 || height < 23 {
		c.narrow(source)
	} else {
		boardHeight := 6
		if wide {
			boardHeight = 7
		}
		c.board(source, 2, boardHeight, wide)
		start := 2 + boardHeight
		end := height - 2
		if wide {
			end -= 6
		}
		state.contentTop, state.contentBottom = start, end
		switch {
		case state.Mode != "":
			c.replacement(source, start, end-start)
		case wide:
			state.contentBottom = end - 11
			c.evidence(source, start, end-start-11, true)
			c.preview(source, end-11, 11)
		default:
			c.evidence(source, start, end-start, false)
		}
		if wide {
			c.coverage(source, height-8, 6)
		}
	}
}

func (c *summaryCanvas) footer(source *SummarySource) {
	state, width, height := c.state, c.width, len(c.rows)
	wide := width >= 120 && height >= 39
	member := state.Member
	if member == "" {
		member = "All members"
	}
	if !wide {
		c.line(height-2, "Coverage: LIMITED | latest 30 msgs/session; excerpts <=4000 runes | c details")
	} else {
		c.line(height-2, fmt.Sprintf("Showing loaded evidence only | %s | %d new | event %d/%d",
			member, state.New, max(0, state.selected()+1), len(state.entries)))
	}
	c.hit(0, height-2, width, "coverage")
	footer := "j/k Enter detail gg/G m member c coverage b Board r refresh ? help Esc back"
	if state.Mode != "" {
		footer = "j/k scroll  PgUp/Dn  m member  c coverage  r refresh  Esc evidence"
	}
	if state.Mode == "picker" {
		footer = "Type search  arrows select  Tab focus  Enter Apply  Esc Cancel"
	}
	c.line(height-1, footer)
	if width < 80 {
		footer = "j/k  Enter detail  m member  c  Esc back"
		c.line(height-1, footer)
	}
	if state.Mode != "picker" {
		controls := []struct{ label, action string }{
			{"m member", "picker"},
			{"c coverage", "coverage"},
			{"b Board", "board"},
			{"r refresh", "refresh"},
			{"? help", "help"},
		}
		for _, control := range controls {
			if x := strings.Index(footer, control.label); x >= 0 && x+len(control.label) <= width {
				if control.action != "refresh" || (!source.BoardStatus.Loading && !source.ChannelStatus.Loading) {
					c.hit(x, height-1, len(control.label), control.action)
				}
			}
		}
	}
}

func (c *summaryCanvas) style() {
	for i, line := range c.rows {
		style := lipgloss.NewStyle().Foreground(c.theme.Text).Background(c.theme.Surface)
		if c.state.selectedRows[i] || strings.Contains(line, "| >") {
			style = style.Background(c.theme.Focus)
		}
		if strings.HasPrefix(line, "+") {
			style = style.Foreground(c.theme.Border)
		}
		if strings.Contains(line, "LIMITED") || strings.Contains(line, "STALE") {
			style = style.Foreground(c.theme.Warning)
		}
		if strings.Contains(line, "FAIL") || strings.Contains(line, "failed:") || strings.Contains(line, "unavailable") {
			style = style.Foreground(c.theme.Danger)
		}
		c.rows[i] = style.Render(line)
	}
}
