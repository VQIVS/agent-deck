package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/vqivs/agent-deck/internal/team"
)

func (c *summaryCanvas) board(source *SummarySource, y, height int, wide bool) {
	var tasks []*team.Task
	if source.Board != nil {
		for i := range source.Board.Tasks {
			task := &source.Board.Tasks[i]
			if c.state.Member == "" || kanbanAssignee(task) == c.state.Member {
				tasks = append(tasks, task)
			}
		}
	}
	status := source.BoardStatus
	title := fmt.Sprintf("Current board / %d tasks", len(tasks))
	if c.state.Member != "" {
		title += " / @" + c.state.Member
	}
	if !status.Loaded {
		title = "Current board / -- tasks"
	}
	if status.Error != "" && status.Loaded {
		title = "Board snapshot / STALE / cached counts"
	}
	c.panel(y, height, title)
	boardAction := "[b Open Board]"
	x := max(2, c.width-len(boardAction)-3)
	defer func() {
		line := c.rows[y+1]
		padding := strings.Repeat(" ", max(0, c.width-x-len(boardAction)-1))
		c.rows[y+1] = ansi.Truncate(line, x, "") + boardAction + padding + "|"
		c.hit(x, y+1, len(boardAction), "board")
	}()
	if !status.Loaded {
		message := "Board state unknown"
		if status.Loading {
			message = "Loading board..."
		}
		if status.Error != "" {
			message = "Board unavailable; task counts unknown"
		}
		c.body(y+1, message)
		if status.Error != "" {
			c.body(y+2, "Read failed: "+status.Error+"; r retry")
		}
		return
	}
	c.boardTasks(tasks, y, height, wide, x)
	if status.Error != "" {
		c.body(y+height-2, "Board refresh failed: "+status.Error+"; r retry")
	}
}

func (c *summaryCanvas) boardTasks(tasks []*team.Task, y, height int, wide bool, x int) {
	counts := map[string]int{}
	for _, task := range tasks {
		counts[task.State]++
	}
	states := make([]string, 0, len(counts))
	for state := range counts {
		states = append(states, state)
	}
	slices.Sort(states)
	parts := make([]string, 0, len(states))
	for _, state := range states {
		parts = append(parts, fmt.Sprintf("%d %s", counts[state], state))
	}
	digest := strings.Join(parts, " / ")
	if len(tasks) == 0 {
		digest = "0 tasks in loaded board"
	}
	if ansi.StringWidth(digest) > c.width-22 {
		digest = ansi.Truncate(digest, c.width-40, "...") + fmt.Sprintf(" +%d states; b Board", len(states))
	}
	if wide {
		c.body(y+1, "STATE          TASK   ASSIGNEE       TITLE")
	} else {
		c.body(y+1, ansi.Truncate(digest, max(1, x-3), "..."))
	}
	for i := 0; i < min(3, len(tasks)); i++ {
		if i == 2 && len(tasks) > 3 {
			c.body(y+2+i, fmt.Sprintf("+%d more tasks; b Open Board", len(tasks)-2))
			break
		}
		task := tasks[i]
		owner := kanbanOwner(kanbanAssignee(task))
		text := summaryCell(task.ID, 7) + summaryCell(task.State, 15) + summaryCell(owner, 12) + task.Title
		if wide {
			text = summaryCell(task.State, 15) + summaryCell(task.ID, 7) + summaryCell(owner, 14) + task.Title
		}
		c.body(y+2+i, text)
	}
	if wide {
		c.body(y+height-2, digest)
	}
}

func summaryPreview(e *SummaryEntry) string {
	if e.Report != nil {
		out := e.Report.Summary
		if len(e.Report.Tests) == 0 {
			out += "; tests reported: not run"
		}
		for _, test := range e.Report.Tests {
			out += "; tests reported: " + strings.ToUpper(test.Outcome)
		}
		if len(e.Report.OpenIssues) > 0 {
			out += fmt.Sprintf("; %d open issue(s)", len(e.Report.OpenIssues))
		}
		return out
	}
	text := strings.ReplaceAll(e.Text, "\n", " ")
	if e.Truncated {
		text = "[truncated] " + text
	}
	return text
}

type summaryEventLine struct {
	text  string
	index int
}

func (c *summaryCanvas) eventLines(wide bool) []summaryEventLine {
	var rows []summaryEventLine
	date := ""
	for i := range c.state.entries {
		e := &c.state.entries[i]
		d := "Unknown time / deterministic source order"
		if !e.At.IsZero() {
			d = e.At.UTC().Format("2006-01-02 UTC")
		}
		if d != date {
			rows = append(rows, summaryEventLine{d, -1})
			date = d
		}
		clock := "--:--"
		if !e.At.IsZero() {
			clock = e.At.UTC().Format("15:04")
		}
		kind, authority := summaryAuthority(e)
		task := e.TaskID
		if task == "" {
			task = "--"
		}
		marker := "  "
		if e.ID == c.state.SelectedID {
			marker = "> "
		}
		if wide {
			marker += " "
			authorWidth := 14
			if c.width < 160 {
				authorWidth = max(8, 14-(160-c.width)/5)
			}
			metadata := marker + summaryCell(clock, 10) + summaryCell(e.Member, authorWidth) +
				summaryCell(task, 7) + summaryCell(kind+" / "+authority, 29)
			rows = append(rows, summaryEventLine{metadata + summaryPreview(e), i})
		} else {
			if c.width < 80 {
				rows = append(rows, summaryEventLine{marker + clock + " " + kind + " / " + authority, i},
					summaryEventLine{"  " + e.Member + " " + task, i}, summaryEventLine{"  " + summaryPreview(e), i})
				continue
			}
			metadata := marker + summaryCell(clock, 6) + summaryCell(e.Member, 12) +
				summaryCell(task, 6) + kind + " / " + authority
			rows = append(rows, summaryEventLine{metadata, i}, summaryEventLine{"  " + summaryPreview(e), i})
		}
	}
	return rows
}

func (c *summaryCanvas) evidence(source *SummarySource, y, height int, wide bool) {
	s := c.state
	title := fmt.Sprintf("Evidence / oldest -> newest / %d loaded / %d/%d selected",
		len(s.entries), max(0, s.selected()+1), len(s.entries))
	if len(s.entries) == 0 && (source.BoardStatus.Loading || source.ChannelStatus.Loading) {
		title = "Evidence / loading / -- selected"
	}
	if s.New > 0 {
		title = fmt.Sprintf("Evidence / +%d new; G newest / %d loaded / %d/%d selected",
			s.New, len(s.entries), max(0, s.selected()+1), len(s.entries))
	}
	c.panel(y, height, title)
	start := y + 1
	if s.Notice != "" && height > 6 {
		c.body(start, s.Notice)
		start++
	}
	if wide {
		c.body(start, "   TIME UTC  AUTHOR        TASK   SOURCE / AUTHORITY           PREVIEW")
		start++
	}
	capacity := max(1, y+height-2-start) // Last interior row belongs to the Details action.
	s.page = max(1, capacity)
	if len(s.entries) == 0 {
		c.emptyEvidence(source, start, capacity)
		return
	}
	c.evidenceRows(y, height, start, capacity, wide)
}

func (c *summaryCanvas) emptyEvidence(source *SummarySource, start, capacity int) {
	s := c.state
	message := "No evidence yet"
	detail := "No matching persisted events or assistant text loaded."
	if s.Member != "" {
		message = "No evidence by @" + s.Member + " in loaded records"
		detail = "[a All members]  [m Choose member]"
	}
	if source.BoardStatus.Loading || source.ChannelStatus.Loading {
		message = "Loading available evidence..."
		detail = "Source results pending; loaded evidence counts are unknown."
	}
	if source.BoardStatus.Error != "" && source.ChannelStatus.Error != "" {
		message = "Evidence unavailable"
		detail = "Board: " + source.BoardStatus.Error + " / Channel: " + source.ChannelStatus.Error
	}
	c.body(start, message)
	if capacity > 1 {
		c.body(start+1, detail)
	}
	if capacity > 3 {
		c.body(start+3, "[r Refresh board/channel]  [b Open Board]")
		c.hit(2, start+3, 25, "refresh")
		c.hit(29, start+3, 14, "board")
	}
}

func (c *summaryCanvas) evidenceRows(y, height, start, capacity int, wide bool) {
	s := c.state
	rows := c.eventLines(wide)
	// Reserve one date row when the viewport starts inside an event group.
	capacity = max(1, capacity-1)
	s.anchorRows(rows, capacity, wide)
	if s.Offset < len(rows) && rows[s.Offset].index >= 0 && capacity > 1 {
		e := s.entries[rows[s.Offset].index]
		date := "Unknown time"
		if !e.At.IsZero() {
			date = e.At.UTC().Format("2006-01-02 UTC")
		}
		c.body(start, date)
		start++
	} else {
		capacity++
	}
	c.drawEvents(rows, start, capacity, wide)
	// The visible action has its own reserved row and never covers evidence.
	action := "[Enter Details]"
	x := max(2, c.width-len(action)-3)
	c.body(y+height-2, "")
	line := c.rows[y+height-2]
	c.rows[y+height-2] = ansi.Truncate(line, x, "") + action + strings.Repeat(" ", max(0, c.width-x-len(action)-1)) + "|"
	c.hit(x, y+height-2, len(action), "detail")
}

func (s *SummaryState) anchorRows(rows []summaryEventLine, capacity int, wide bool) {
	selectedStart, selectedEnd := 0, 0
	for i, row := range rows {
		if row.index == s.selected() {
			if selectedEnd == 0 {
				selectedStart = i
			}
			selectedEnd = i + 1
		}
	}
	if selectedStart < s.Offset {
		s.Offset = selectedStart
	}
	if selectedEnd > s.Offset+capacity {
		s.Offset = selectedEnd - capacity
	}
	s.Offset = max(0, min(s.Offset, max(0, len(rows)-capacity)))
	if !wide {
		for s.Offset > 0 && s.Offset < len(rows) && rows[s.Offset].index >= 0 &&
			rows[s.Offset-1].index == rows[s.Offset].index {
			s.Offset++
		}
	}
}

func (c *summaryCanvas) drawEvents(rows []summaryEventLine, start, capacity int, wide bool) {
	s := c.state
	for i := 0; i < capacity && s.Offset+i < len(rows); i++ {
		row := rows[s.Offset+i]
		if !wide && row.index >= 0 {
			remaining := 0
			for j := s.Offset + i; j < len(rows) && rows[j].index == row.index; j++ {
				remaining++
			}
			if remaining > capacity-i {
				break
			}
		}
		c.body(start+i, row.text)
		if row.index >= 0 && row.index == s.selected() {
			s.selectedRows[start+i] = true
		}
		if row.index >= 0 {
			c.hit(2, start+i, c.width-4, fmt.Sprintf("event:%d", row.index))
		}
	}
}

func summaryDetail(e *SummaryEntry, source *SummarySource) []string {
	kind, authority := summaryAuthority(e)
	lines := []string{
		"Reported at " + summaryTime(e.At), "Author: " + e.Member + " | Task: " + e.TaskID,
		"Source: " + kind + " / " + authority, "Source ID: " + e.ID,
	}
	if source.Board != nil {
		for i := range source.Board.Tasks {
			task := &source.Board.Tasks[i]
			if task.ID == e.TaskID {
				lines = append(lines, "Current board: "+task.State)
				break
			}
		}
	}
	if e.Report != nil {
		lines = append(lines, "", "SUMMARY", e.Report.Summary, "", "CRITERIA (worker-reported)")
		for _, item := range e.Report.Checklist {
			lines = append(lines, fmt.Sprintf("%s complete=%t (reported): %s", item.ID, item.Complete, item.Evidence))
		}
		lines = append(lines, "", "TEST CLAIMS (not independently verified)")
		if len(e.Report.Tests) == 0 {
			lines = append(lines, "No tests reported")
		}
		for _, test := range e.Report.Tests {
			lines = append(lines, fmt.Sprintf("%s | %s | %s (reported) | %s",
				test.CriterionID, test.Command, strings.ToUpper(test.Outcome), test.Details))
		}
		lines = append(lines, "", "OPEN ISSUES")
		lines = append(lines, e.Report.OpenIssues...)
		lines = append(lines, "Reported completion does not imply review acceptance or independently passing tests.")
	} else {
		lines = append(lines, "", "AVAILABLE CONTENT", e.Text)
	}
	if e.Truncated {
		lines = append(lines, "Excerpt truncated at 4000 runes; earlier/remaining content may be missing.")
	}
	return lines
}

func (c *summaryCanvas) preview(source *SummarySource, y, height int) {
	idx := c.state.selected()
	title := "Selected evidence / Nothing selected"
	if idx >= 0 {
		e := c.state.entries[idx]
		kind, authority := summaryAuthority(&e)
		title = "Selected evidence / " + e.TaskID + " / " + e.Member + " / " +
			kind + " / " + authority + " [Enter Full details]"
	}
	c.panel(y, height, title)
	if idx < 0 {
		c.body(y+1, "Nothing selected")
		return
	}
	lines := summaryDetail(&c.state.entries[idx], source)
	e := &c.state.entries[idx]
	if e.Report != nil {
		lines = []string{
			"Reported at " + summaryTime(e.At) + " | Source: structured worker report", "SUMMARY     " + e.Report.Summary,
		}
		if source.Board != nil {
			for i := range source.Board.Tasks {
				if source.Board.Tasks[i].ID == e.TaskID {
					lines[0] += " | Current board: " + source.Board.Tasks[i].State
					break
				}
			}
		}
		for _, item := range e.Report.Checklist {
			lines = append(lines, fmt.Sprintf("CRITERIA    %s complete=%t (reported): %s",
				item.ID, item.Complete, item.Evidence))
		}
		for _, test := range e.Report.Tests {
			lines = append(lines, fmt.Sprintf("TEST CLAIM  %s | %s | %s (reported) | %s",
				test.CriterionID, test.Command, strings.ToUpper(test.Outcome), test.Details))
		}
		for _, issue := range e.Report.OpenIssues {
			lines = append(lines, "OPEN ISSUE  "+issue)
		}
		lines = append(lines, "Reported completion does not imply review acceptance or independently passing tests.",
			"Source ID: "+e.ID)
	}
	for i := 0; i < min(height-3, len(lines)); i++ {
		c.body(y+i+1, lines[i])
	}
	c.body(y+height-2, fmt.Sprintf("Available content 1-%d/%d | Enter opens scrollable full details",
		min(height-3, len(lines)), len(lines)))
	c.hit(2, y, c.width-4, "detail")
}

func summaryCoverage(source *SummarySource) []string {
	lines := []string{
		"Coverage: LIMITED", "Board: " + summaryStatusText("", source.BoardStatus),
		"Board last success: " + summaryTime(source.BoardStatus.LastSuccess), "Board error: " + source.BoardStatus.Error,
		"Channel: " + summaryStatusText("", source.ChannelStatus),
		"Channel last success: " + summaryTime(source.ChannelStatus.LastSuccess),
		"Channel error: " + source.ChannelStatus.Error,
		"Board and channel: supplied persisted records; archive completeness unknown.",
		"Refresh scope: board + channel only. Session windows retain their own load time.",
	}
	for i := range source.Assistants {
		a := &source.Assistants[i]
		limit := a.Limit
		if limit <= 0 {
			limit = 30
		}
		status := summaryStatusText("Session", a.Status)
		if a.Session == "" {
			status = "unbound session"
		}
		entries := []SummaryEntry{}
		summaryAssistant(a, func(e SummaryEntry) { entries = append(entries, e) })
		truncated := 0
		for _, e := range entries {
			if e.Truncated {
				truncated++
			}
		}
		counts := fmt.Sprintf("Fetched %d messages / latest %d window; %d assistant excerpts; %d truncated",
			min(len(a.Messages), limit), limit, len(entries), truncated)
		if !a.Status.Loaded {
			counts = fmt.Sprintf("Fetched count unknown / latest %d window; %d supplied assistant excerpts; %d truncated",
				limit, len(entries), truncated)
		}
		lines = append(lines, "", "Member: "+a.Member, "Directory: "+a.Directory, "Session: "+a.Session,
			"Status: "+status, "Last successful load: "+summaryTime(a.Status.LastSuccess), "Error: "+a.Status.Error, counts)
		if len(entries) > 0 {
			lines = append(lines, "First excerpt time: "+summaryTime(entries[0].At),
				"Last excerpt time: "+summaryTime(entries[len(entries)-1].At))
		}
	}
	return append(lines, "Assistant text only; each excerpt <=4000 runes. Earlier messages may be missing.",
		"Loaded totals are not archive totals. Persisted and reported do not mean independently verified.")
}

func (c *summaryCanvas) coverage(source *SummarySource, y, height int) {
	c.panel(y, height, "Coverage / LIMITED [c Details]")
	lines := []string{
		"Board and channel: supplied persisted records; not a reconstructed archive.",
		"Session windows: latest 30 fetched messages/member; assistant text only; each excerpt <=4000 runes.",
		"Refresh scope: board + channel only. Transcript windows retain their own load time.",
		"Reported claims are not independently verified completion. Earlier messages may be missing.",
	}
	for i, line := range lines {
		c.body(y+i+1, line)
	}
	c.hit(2, y, c.width-4, "coverage")
}

func (c *summaryCanvas) narrow(source *SummarySource) {
	c.state.contentTop, c.state.contentBottom = 4, len(c.rows)-2
	c.line(2, summaryStatusText("Board", source.BoardStatus)+" | b Board")
	c.line(3, "Board: assignee / Events: author | c coverage")
	if c.state.Mode != "" {
		c.replacement(source, 4, len(c.rows)-6)
	} else {
		c.evidence(source, 4, len(c.rows)-6, false)
	}
}
