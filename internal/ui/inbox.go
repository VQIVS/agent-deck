package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/vqivs/agent-deck/internal/state"
)

// InboxIdentity binds entries to a member and its exact session scope.
type InboxIdentity struct {
	Agent     string
	Directory string
	Session   string
}

// InboxReport is supplied by the report store; inferred reports never imply verified success.
type InboxReport struct {
	ID       string
	Text     string
	At       time.Time
	Inferred bool
}

// InboxSource is one member's current reducer state and explicitly supplied done reports.
type InboxSource struct {
	Identity InboxIdentity
	State    *state.State
	Reports  []InboxReport
}

// InboxEntry is a stable projection; RequestID is only populated for actionable requests.
type InboxEntry struct {
	ID        string
	Identity  InboxIdentity
	Kind      string
	RequestID string
	Title     string
	Detail    string
	At        time.Time
	Inferred  bool
}

func inboxID(identity InboxIdentity, kind, key string) string {
	return fmt.Sprintf("%q/%q/%q/%q/%q", identity.Agent, identity.Directory, identity.Session, kind, key)
}

func ProjectInbox(sources []InboxSource, now time.Time) []InboxEntry {
	entries := map[string]*InboxEntry{}
	for i := range sources {
		projected := sourceEntries(&sources[i], now)
		for j := range projected {
			entry := &projected[j]
			old, exists := entries[entry.ID]
			if !exists || entry.At.After(old.At) {
				entries[entry.ID] = entry
			}
		}
	}
	result := make([]InboxEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].At.Equal(result[j].At) {
			return result[i].At.After(result[j].At)
		}
		return result[i].ID < result[j].ID
	})
	return result
}

func sourceEntries(source *InboxSource, now time.Time) []InboxEntry {
	var entries []InboxEntry
	s := source.State
	if s != nil {
		entries = append(entries, inboxRequests(source.Identity, s)...)
		if s.Error != "" || s.Status == "ERROR" {
			entries = append(entries, newInboxEntry(source.Identity, "error", s.Since.Format(time.RFC3339Nano),
				"Session error", s.Error, s.Since))
		}
		if s.Stalled(now) {
			at := s.ActivitySince().Add(s.StallAfter)
			entries = append(entries, newInboxEntry(source.Identity, "stall", at.Format(time.RFC3339Nano),
				"Session stalled", "No meaningful part updates while busy", at))
		}
	}
	for _, report := range source.Reports {
		if report.ID == "" {
			continue
		}
		entry := newInboxEntry(source.Identity, "done", report.ID, "Done report", report.Text, report.At)
		entry.Inferred = report.Inferred
		if report.Inferred {
			entry.Title = "Done report (inferred)"
		}
		entries = append(entries, entry)
	}
	return entries
}

func newInboxEntry(identity InboxIdentity, kind, key, title, detail string, at time.Time) InboxEntry {
	return InboxEntry{
		ID: inboxID(identity, kind, key), Identity: identity, Kind: kind,
		Title: title, Detail: detail, At: at,
	}
}

func inboxRequests(identity InboxIdentity, s *state.State) []InboxEntry {
	var entries []InboxEntry
	for _, request := range s.Permissions {
		if request.ID == "" || request.SessionID != identity.Session {
			continue
		}
		entry := newInboxEntry(identity, "permission", request.ID, "Permission: "+request.Permission,
			strings.Join(request.Patterns, ", "), s.WaitingSince)
		entry.RequestID = request.ID
		entries = append(entries, entry)
	}
	for _, request := range s.Questions {
		if request.ID == "" || request.SessionID != identity.Session {
			continue
		}
		var details []string
		for _, question := range request.Questions {
			details = append(details, question.Header+": "+question.Question)
		}
		entry := newInboxEntry(identity, "question", request.ID, "Question", strings.Join(details, " · "), s.WaitingSince)
		entry.RequestID = request.ID
		entries = append(entries, entry)
	}
	return entries
}

// InboxFilter matches a kind, member name and case-insensitive text query; empty fields match all.
type InboxFilter struct {
	Kind  string
	Agent string
	Query string
}

func FilterInbox(entries []InboxEntry, filter InboxFilter) []InboxEntry {
	var result []InboxEntry
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	for i := range entries {
		entry := &entries[i]
		if (filter.Kind != "" && entry.Kind != filter.Kind) ||
			(filter.Agent != "" && entry.Identity.Agent != filter.Agent) {
			continue
		}
		text := strings.Join([]string{
			entry.Identity.Agent, entry.Identity.Directory, entry.Identity.Session,
			entry.Kind, entry.Title, entry.Detail,
		}, " ")
		if query == "" || strings.Contains(strings.ToLower(text), query) {
			result = append(result, *entry)
		}
	}
	return result
}

// InboxSelection keeps a selected identity across insertion, sorting and filtering.
type InboxSelection struct {
	ID    string
	Index int
}

func (s *InboxSelection) Reconcile(entries []InboxEntry) {
	if len(entries) == 0 {
		s.ID, s.Index = "", 0
		return
	}
	for i := range entries {
		if entries[i].ID == s.ID {
			s.Index = i
			return
		}
	}
	s.Index = max(0, min(s.Index, len(entries)-1))
	s.ID = entries[s.Index].ID
}

func (s *InboxSelection) Move(entries []InboxEntry, delta int) {
	s.Reconcile(entries)
	if len(entries) == 0 {
		return
	}
	s.Index = max(0, min(s.Index+delta, len(entries)-1))
	s.ID = entries[s.Index].ID
}

func (s *InboxSelection) Selected(entries []InboxEntry) (InboxEntry, bool) {
	s.Reconcile(entries)
	if len(entries) == 0 {
		return InboxEntry{}, false
	}
	return entries[s.Index], true
}
