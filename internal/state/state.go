package state

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	api "github.com/qivs/agent-deck/internal/opencode"
)

const MaxParts = 160

// State reduces snapshots and events into bounded transcript and activity state.
type State struct {
	Status       string
	Since        time.Time
	Parts        []api.Part
	Error        string
	Seen         map[string]bool
	Permissions  []api.Permission
	Questions    []api.Question
	WaitingSince time.Time
}

func New() State { return State{Status: "UNKNOWN", Since: time.Now(), Seen: map[string]bool{}} }
func (s *State) SetStatus(status string, now time.Time) bool {
	status = strings.ToUpper(status)
	if status == "RETRY" {
		status = "BUSY"
	}
	if status == "" {
		status = "IDLE"
	}
	if s.Status == status {
		return false
	}
	s.Status = status
	s.Since = now
	if status != "ERROR" {
		s.Error = ""
	}
	return true
}

func (s *State) Upsert(source *api.Part) {
	p := *source
	if p.Type != "text" && p.Type != "reasoning" && p.Type != "tool" {
		return
	}
	// Bound memory even for very large tool outputs / long-running responses.
	if len(p.Text) > 24000 {
		p.Text = p.Text[len(p.Text)-24000:]
	}
	if len(p.State.Output) > 12000 {
		p.State.Output = p.State.Output[len(p.State.Output)-12000:]
	}
	for i := range s.Parts {
		if s.Parts[i].ID == p.ID {
			s.Parts[i] = p
			return
		}
	}
	s.Parts = append(s.Parts, p)
	if len(s.Parts) > MaxParts {
		s.Parts = s.Parts[len(s.Parts)-MaxParts:]
	}
}

func (s *State) Snapshot(messages []api.Message) {
	s.Parts = nil
	for i := range messages {
		m := &messages[i]
		for j := range m.Parts {
			p := m.Parts[j]
			p.Role = m.Info.Role
			p.Created = m.Info.Time.Created
			s.Upsert(&p)
		}
	}
}

type eventProperties struct {
	SessionID string          `json:"sessionID"`
	Part      api.Part        `json:"part"`
	PartID    string          `json:"partID"`
	MessageID string          `json:"messageID"`
	Field     string          `json:"field"`
	Delta     string          `json:"delta"`
	Status    api.Status      `json:"status"`
	Error     json.RawMessage `json:"error"`
}

func (s *State) Apply(e api.Event, session string, now time.Time) bool {
	var p eventProperties
	if json.Unmarshal(e.Properties, &p) != nil {
		return false
	}
	if p.SessionID == "" {
		p.SessionID = p.Part.SessionID
	}
	if p.SessionID != session {
		return false
	}
	if strings.HasPrefix(e.Type, "permission.") || strings.HasPrefix(e.Type, "question.") {
		return s.ApplyRequest(e, session, now)
	}
	if s.duplicate(e.ID) {
		return false
	}
	return s.applyEvent(e.Type, &p, now)
}

func (s *State) duplicate(id string) bool {
	if id == "" {
		return false
	}
	if s.Seen[id] {
		return true
	}
	if len(s.Seen) > 2048 || s.Seen == nil {
		s.Seen = map[string]bool{}
	}
	s.Seen[id] = true
	return false
}

func (s *State) applyEvent(kind string, p *eventProperties, now time.Time) bool {
	switch kind {
	case "session.status":
		return s.SetStatus(p.Status.Type, now)
	case "session.idle":
		return s.SetStatus("idle", now)
	case "session.error":
		s.SetStatus("error", now)
		s.Error = string(p.Error)
		return true
	case "message.part.updated":
		s.Upsert(&p.Part)
	case "message.part.delta":
		s.applyDelta(p)
	case "message.part.removed":
		for i := range s.Parts {
			if s.Parts[i].ID == p.PartID {
				s.Parts = append(s.Parts[:i], s.Parts[i+1:]...)
				break
			}
		}
	case "message.removed":
		for i := len(s.Parts) - 1; i >= 0; i-- {
			if s.Parts[i].MessageID == p.MessageID {
				s.Parts = append(s.Parts[:i], s.Parts[i+1:]...)
			}
		}
	}
	return false
}

func (s *State) applyDelta(p *eventProperties) {
	if p.Field != "text" {
		return
	}
	for i := range s.Parts {
		if s.Parts[i].ID == p.PartID {
			part := s.Parts[i]
			part.Text += p.Delta
			s.Upsert(&part)
			return
		}
	}
	s.Upsert(&api.Part{ID: p.PartID, MessageID: p.MessageID, Type: "text", Text: p.Delta})
}

func (s *State) Tool() string {
	for i := len(s.Parts) - 1; i >= 0; i-- {
		p := s.Parts[i]
		if p.Type == "tool" && (p.State.Status == "running" || p.State.Status == "pending") {
			return p.Tool + " " + string(p.State.Input)
		}
	}
	return "—"
}

func (s *State) Tail() string {
	var b strings.Builder
	for i := range s.Parts {
		p := &s.Parts[i]
		if p.Type == "tool" {
			fmt.Fprintf(&b, "› %s %s [%s]\n", p.Tool, p.State.Input, p.State.Status)
			if p.State.Error != "" {
				fmt.Fprintln(&b, p.State.Error)
			}
		} else if p.Text != "" {
			fmt.Fprintln(&b, p.Text)
		}
	}
	if s.Error != "" {
		fmt.Fprintln(&b, "ERROR: "+s.Error)
	}
	return b.String()
}
