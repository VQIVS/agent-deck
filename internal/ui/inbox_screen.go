package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// InboxScreen holds local navigation and identity-bound confirmation state.
type InboxScreen struct {
	Open      bool
	Selection InboxSelection
	Filter    InboxFilter
	Reports   map[InboxIdentity][]InboxReport
	Flights   *InboxFlights
	searching bool
	pending   *InboxEntry
	rejecting bool
	reason    string
}

func (m *Model) inboxEntries(screen *InboxScreen) []InboxEntry {
	if screen.Flights == nil {
		screen.Flights = &InboxFlights{}
	}
	sources := make([]InboxSource, 0, len(m.panes))
	for i := range m.panes {
		p := &m.panes[i]
		ids := make([]string, 0, len(p.State.Permissions))
		for _, request := range p.State.Permissions {
			ids = append(ids, request.ID)
		}
		screen.Flights.Observe(p.Agent.Dir, p.Agent.Session, ids)
		identity := InboxIdentity{Agent: p.Agent.Name, Directory: p.Agent.Dir, Session: p.Agent.Session}
		sources = append(sources, InboxSource{Identity: identity, State: &p.State, Reports: screen.Reports[identity]})
	}
	entries := FilterInbox(ProjectInbox(sources, time.Now()), screen.Filter)
	entries = append(entries, m.poolInboxEntries()...)
	screen.Selection.Reconcile(entries)
	return entries
}

func (m *Model) inboxView(screen *InboxScreen) string {
	entries := m.inboxEntries(screen)
	m.zones = nil
	header := "Inbox · / search · Tab kind · Esc back"
	if screen.searching {
		header = "Search: " + screen.Filter.Query + " · Enter finish"
	}
	if screen.Filter.Kind != "" {
		header += " · " + screen.Filter.Kind
	}
	foot := "Enter open · y approve once · Y always · x reject · a attach question · n nudge"
	if screen.pending != nil {
		foot = "Always approve " + screen.pending.Title + "? y/N"
		if screen.rejecting {
			foot = "Reject reason: " + screen.reason + " · Enter submit · Esc cancel"
		}
	}
	if m.opts.ReadOnly {
		foot = "READ ONLY · Enter open · / search · Tab kind"
	}
	height := max(0, m.h-4)
	body := RenderInbox(entries, screen.Selection.ID, m.w, height)
	start := inboxStart(entries, screen.Selection.ID, height)
	for i := start; i < len(entries) && (i-start)*2 < height; i++ {
		m.zones = append(m.zones, zone{
			Rect:   Rect{0, 1 + (i-start)*2, m.w, min(2, height-(i-start)*2)},
			Action: "inbox-select:" + entries[i].ID, Pane: -1,
		})
	}
	clip := func(text string) string { return RenderInboxLine(text, m.w) }
	return clip(header) + "\n" + body + "\n" + clip(foot) + "\n" + clip(m.note)
}

func inboxStart(entries []InboxEntry, selectedID string, height int) int {
	selected := 0
	for i := range entries {
		if entries[i].ID == selectedID {
			selected = i
			break
		}
	}
	rows := max(1, height/2)
	return max(0, min(selected-rows/2, len(entries)-rows))
}

func (m *Model) inboxKey(screen *InboxScreen, v tea.KeyMsg) tea.Cmd {
	if screen.pending != nil {
		return m.inboxConfirmKey(screen, v)
	}
	if screen.searching {
		editInboxText(&screen.Filter.Query, v)
		if v.String() == "enter" || v.String() == "esc" {
			screen.searching = false
		}
		return nil
	}
	entries := m.inboxEntries(screen)
	switch v.String() {
	case "esc", "i", "q":
		screen.Open = false
	case "up", "k":
		screen.Selection.Move(entries, -1)
	case "down", "j":
		screen.Selection.Move(entries, 1)
	case "/":
		screen.searching = true
	case "tab":
		kinds := []string{"", "permission", "question", "error", "stall", "done"}
		for i, kind := range kinds {
			if kind == screen.Filter.Kind {
				screen.Filter.Kind = kinds[(i+1)%len(kinds)]
				break
			}
		}
	default:
		if entry, ok := screen.Selection.Selected(entries); ok {
			return m.inboxAction(screen, &entry, v.String())
		}
	}
	return nil
}

func (m *Model) inboxClick(screen *InboxScreen, z zone, ok bool) tea.Cmd {
	if !ok || screen.pending != nil || !strings.HasPrefix(z.Action, "inbox-select:") {
		return nil
	}
	id := strings.TrimPrefix(z.Action, "inbox-select:")
	entries := m.inboxEntries(screen)
	for i := range entries {
		if entries[i].ID == id {
			screen.Selection.ID = id
			break
		}
	}
	return nil
}

func (m *Model) inboxPane(entry *InboxEntry) int {
	for i := range m.panes {
		a := m.panes[i].Agent
		if a.Name == entry.Identity.Agent && a.Dir == entry.Identity.Directory && a.Session == entry.Identity.Session {
			return i
		}
	}
	return -1
}

func (m *Model) inboxAction(screen *InboxScreen, entry *InboxEntry, key string) tea.Cmd {
	if entry.Kind == "pool" && key == "enter" {
		m.board.Open = true
		screen.Open = false
		return m.readBoard()
	}
	index := m.inboxPane(entry)
	if index < 0 {
		m.note = "Inbox member is no longer available"
		return nil
	}
	if key == "enter" {
		m.focus, m.zoom, screen.Open = index, true, false
		return nil
	}
	if m.opts.ReadOnly {
		m.note = "Read-only: inbox actions disabled"
		return nil
	}
	if entry.Kind == "permission" {
		switch key {
		case "y":
			return m.inboxReply(screen, entry, "once", "")
		case "Y", "x":
			copyEntry := *entry
			screen.pending, screen.rejecting, screen.reason = &copyEntry, key == "x", ""
		}
	}
	if key == "a" && entry.Kind == "question" {
		m.focus, screen.Open = index, false
		return m.sessionKey("a")
	}
	if key == "n" {
		m.focus, screen.Open = index, false
		return m.sessionKey("n")
	}
	return nil
}

func (m *Model) inboxConfirmKey(screen *InboxScreen, v tea.KeyMsg) tea.Cmd {
	key := v.String()
	if screen.rejecting && key != "enter" && key != "esc" {
		editInboxText(&screen.reason, v)
		return nil
	}
	entry := screen.pending
	screen.pending = nil
	if screen.rejecting {
		screen.rejecting = false
		if key == "enter" {
			return m.inboxReply(screen, entry, "reject", strings.TrimSpace(screen.reason))
		}
	} else if key == "y" || key == "Y" {
		return m.inboxReply(screen, entry, "always", "")
	}
	return nil
}

func (m *Model) inboxReply(screen *InboxScreen, entry *InboxEntry, reply, reason string) tea.Cmd {
	if m.opts.ReadOnly {
		m.note = "Read-only: inbox actions disabled"
		return nil
	}
	index := m.inboxPane(entry)
	if index >= 0 {
		return m.replyPermissionTarget(permissionTarget{
			Dir: entry.Identity.Directory, ID: entry.RequestID, Session: entry.Identity.Session,
		}, reply, reason)
	}
	m.note = "Permission is no longer pending for this member"
	return nil
}

func editInboxText(text *string, v tea.KeyMsg) {
	switch {
	case v.Type == tea.KeyBackspace:
		runes := []rune(*text)
		if len(runes) > 0 {
			*text = string(runes[:len(runes)-1])
		}
	case v.Type == tea.KeySpace:
		*text += " "
	case v.Type == tea.KeyRunes && len(*text) < 4096:
		*text += string(v.Runes)
	}
}

// RenderInboxLine sanitizes terminal content and bounds one screen chrome line.
func RenderInboxLine(text string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(inboxLine(text), width, "…")
}
