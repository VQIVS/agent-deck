package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func summaryWrap(lines []string, width int) []string {
	var out []string
	for _, line := range lines {
		for _, part := range strings.Split(line, "\n") {
			out = append(out, strings.Split(ansi.Hardwrap(inboxLine(part), max(1, width), true), "\n")...)
		}
	}
	return out
}

func summaryPickerOptions(source *SummarySource, s *SummaryState) []string {
	options := append([]string{""}, summaryMembers(source)...)
	return slices.DeleteFunc(options, func(member string) bool {
		label := member
		if label == "" {
			label = "All members"
		}
		return !strings.Contains(strings.ToLower(label), strings.ToLower(s.pickerSearch))
	})
}

func (c *summaryCanvas) replacement(source *SummarySource, y, height int) {
	s := c.state
	title := "Help / Summary"
	lines := []string{
		"j/k or arrows: select evidence; in detail scroll one line",
		"PgUp/PgDn or Ctrl+u/d: page",
		"gg/Home oldest; G/End newest known-time record",
		"Tab/Shift+Tab: exact member scope; m searchable member picker",
		"Board filters assignee; evidence filters author. Quiet and departed members remain selectable.",
		"Enter: full selected evidence details; Esc/q: close detail, then Dashboard",
		"c: coverage details; a: All members; b: Open Board",
		"r: refresh board/channel only; session windows do not refresh",
		"Click evidence selects only; visible Details opens. Wheel scrolls three lines.",
		"Only loaded records are available. REPORT is a worker claim; EXCERPT is unverified.",
	}
	if s.Mode == "detail" {
		idx := s.selected()
		if idx >= 0 {
			e := &s.entries[idx]
			kind, _ := summaryAuthority(e)
			title = "Evidence details / " + kind + " / " + e.Member
			lines = summaryDetail(e, source)
		} else {
			title = "Evidence details"
			lines = []string{"Nothing selected"}
		}
	}
	if s.Mode == "coverage" {
		title = "Coverage details / LIMITED"
		lines = summaryCoverage(source)
	}
	if s.Mode == "picker" {
		c.picker(source, y, height)
		return
	}
	c.panel(y, height, title)
	start := y + 1
	if s.Notice != "" && height > 5 {
		c.body(start, s.Notice)
		start++
	}
	if s.Mode == "detail" && s.selected() >= 0 {
		e := &s.entries[s.selected()]
		kind, authority := summaryAuthority(e)
		c.body(start, summaryTime(e.At)+" | "+kind+" / "+authority+" | metadata below is scrollable")
		start++
	}
	wrapped := summaryWrap(lines, c.width-4)
	capacity := max(0, y+height-2-start)
	s.DetailOffset = max(0, min(s.DetailOffset, max(0, len(wrapped)-capacity)))
	if s.Mode == "coverage" {
		context := "Sources"
		for i := 0; i <= s.DetailOffset && i < len(wrapped); i++ {
			if strings.HasPrefix(wrapped[i], "Member: ") {
				context = strings.TrimPrefix(wrapped[i], "Member: ")
			}
		}
		c.line(y, "+- "+summaryCell("Coverage details / LIMITED / "+context, max(0, c.width-5))+"-+")
	}
	for i := 0; i < capacity && i+s.DetailOffset < len(wrapped); i++ {
		c.body(start+i, wrapped[i+s.DetailOffset])
	}
	c.body(y+height-2, fmt.Sprintf("Available lines %d-%d/%d | Esc return to evidence",
		min(len(wrapped), s.DetailOffset+1), min(len(wrapped), s.DetailOffset+capacity), len(wrapped)))
}

func (c *summaryCanvas) picker(source *SummarySource, y, height int) {
	s := c.state
	c.panel(y, height, "Member picker / exact identity")
	focus := ""
	if s.pickerFocus == 0 {
		focus = "> "
	}
	runes := []rune(s.pickerSearch)
	s.pickerCursor = max(0, min(s.pickerCursor, len(runes)))
	c.body(y+1, focus+"Search: "+string(runes[:s.pickerCursor])+"_"+string(runes[s.pickerCursor:]))
	options := summaryPickerOptions(source, s)
	s.pickerIndex = max(0, min(s.pickerIndex, len(options)-1))
	capacity := max(1, height-5)
	start := max(0, s.pickerIndex-capacity+1)
	if len(options) == 0 {
		c.body(y+2, "No matching members")
	}
	row := y + 2
	for i := start; i < len(options) && row < y+height-3; i++ {
		member := options[i]
		label := member
		if label == "" {
			label = "All members"
		}
		count := len(summaryEntries(source, member))
		status := "departed"
		currentAssistant := slices.ContainsFunc(source.Assistants, func(a SummaryAssistant) bool {
			return a.Member == member
		})
		if slices.Contains(source.Members, member) || currentAssistant {
			status = "current"
		}
		if member == "" {
			status = "scope"
		}
		marker := "  "
		if i == s.pickerIndex {
			marker = "> "
		}
		option := marker + label + fmt.Sprintf(" / %d events / %s", count, status)
		for _, line := range summaryWrap([]string{option}, c.width-4) {
			if row >= y+height-3 {
				break
			}
			c.body(row, line)
			c.hit(2, row, c.width-4, fmt.Sprintf("option:%d", i))
			row++
		}
	}
	c.body(y+height-2, "[Apply / Enter]   [Cancel / Esc]  Tab focus")
	c.hit(2, y+height-2, 15, "apply")
	c.hit(20, y+height-2, 14, "cancel")
}

func SummaryKey(source *SummarySource, s *SummaryState, key tea.KeyMsg, pageHeight int) bool {
	if s == nil {
		return key.String() == "esc" || key.String() == "q"
	}
	s.sync(source)
	k := key.String()
	if s.Mode == "picker" {
		return summaryPickerKey(source, s, key)
	}
	if k != "g" {
		s.firstG = false
	}
	switch k {
	case "esc", "q":
		if s.Mode != "" {
			s.Mode = ""
			s.DetailOffset = 0
			return false
		}
		return true
	case "m":
		s.Mode = "picker"
		s.pickerSearch = ""
		s.pickerCursor = 0
		s.pickerIndex = 0
		s.pickerFocus = 0
	case "a":
		s.filter(source, "")
	case "tab", "shift+tab":
		s.cycleMember(source, k == "shift+tab")
	case "enter":
		s.openDetail()
	case "c":
		if s.Mode == "coverage" {
			s.Mode = ""
		} else {
			s.Mode = "coverage"
		}
		s.DetailOffset = 0
	case "?":
		s.Mode = "help"
		s.DetailOffset = 0
	default:
		s.navigationKey(k, pageHeight)
	}
	return false
}

func (s *SummaryState) openDetail() {
	if s.Mode == "" && s.selected() >= 0 {
		s.Mode = "detail"
		s.DetailOffset = 0
	}
}

func (s *SummaryState) cycleMember(source *SummarySource, reverse bool) {
	members := append([]string{""}, summaryMembers(source)...)
	i := max(0, slices.Index(members, s.Member))
	step := 1
	if reverse {
		step = -1
	}
	s.filter(source, members[(i+step+len(members))%len(members)])
}

func (s *SummaryState) navigationKey(k string, pageHeight int) {
	switch k {
	case "j", "down":
		s.move(1)
	case "k", "up":
		s.move(-1)
	case "pgdown", "ctrl+d":
		s.move(max(1, min(pageHeight-3, s.page)))
	case "pgup", "ctrl+u":
		s.move(-max(1, min(pageHeight-3, s.page)))
	case "home":
		s.end(false)
	case "G", "end":
		s.end(true)
	case "g":
		if s.firstG {
			s.end(false)
			s.firstG = false
		} else {
			s.firstG = true
		}
	}
}

func (s *SummaryState) move(delta int) {
	if s.Mode != "" {
		s.DetailOffset = max(0, s.DetailOffset+delta)
		return
	}
	if len(s.entries) == 0 {
		return
	}
	i := max(0, min(len(s.entries)-1, s.selected()+delta))
	s.SelectedID = s.entries[i].ID
}

func (s *SummaryState) end(newest bool) {
	if s.Mode != "" {
		if newest {
			s.DetailOffset = 1 << 25
		} else {
			s.DetailOffset = 0
		}
		return
	}
	if newest {
		s.selectNewest()
	} else if len(s.entries) > 0 {
		s.SelectedID = s.entries[0].ID
		s.Offset = 0
	}
}

func summaryPickerKey(source *SummarySource, s *SummaryState, key tea.KeyMsg) bool {
	options := summaryPickerOptions(source, s)
	runes := []rune(s.pickerSearch)
	s.pickerCursor = max(0, min(s.pickerCursor, len(runes)))
	switch key.String() {
	case "esc":
		s.Mode = ""
	case "enter":
		if s.pickerFocus == 3 {
			s.Mode = ""
			return false
		}
		if len(options) > 0 {
			s.filter(source, options[max(0, min(s.pickerIndex, len(options)-1))])
		}
	case "up":
		s.pickerIndex = max(0, s.pickerIndex-1)
	case "down":
		s.pickerIndex = min(max(0, len(options)-1), s.pickerIndex+1)
	case "pgup":
		s.pickerIndex = max(0, s.pickerIndex-5)
	case "pgdown":
		s.pickerIndex = min(max(0, len(options)-1), s.pickerIndex+5)
	case "tab":
		s.pickerFocus = (s.pickerFocus + 1) % 4
	case "shift+tab":
		s.pickerFocus = (s.pickerFocus + 3) % 4
	default:
		s.pickerEditKey(key, runes)
	}
	return false
}

func (s *SummaryState) pickerEditKey(key tea.KeyMsg, runes []rune) {
	switch key.String() {
	case "ctrl+u":
		s.pickerSearch = ""
		s.pickerCursor = 0
		s.pickerIndex = 0
	case "home":
		s.pickerCursor = 0
	case "end":
		s.pickerCursor = len(runes)
	case "left":
		s.pickerCursor = max(0, s.pickerCursor-1)
	case "right":
		s.pickerCursor = min(len(runes), s.pickerCursor+1)
	case "ctrl+d", "delete":
		if s.pickerCursor < len(runes) {
			s.pickerSearch = string(append(runes[:s.pickerCursor], runes[s.pickerCursor+1:]...))
		}
		s.pickerIndex = 0
	case "backspace", "ctrl+h":
		if s.pickerCursor > 0 {
			s.pickerSearch = string(append(runes[:s.pickerCursor-1], runes[s.pickerCursor:]...))
			s.pickerCursor--
		}
		s.pickerIndex = 0
	default:
		if key.Type == tea.KeyRunes {
			value := append([]rune{}, runes[:s.pickerCursor]...)
			value = append(value, key.Runes...)
			value = append(value, runes[s.pickerCursor:]...)
			s.pickerSearch = string(value)
			s.pickerCursor += len(key.Runes)
			s.pickerIndex = 0
		}
	}
}
