package state

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	api "github.com/qivs/agent-deck/internal/opencode"
)

func (s *State) Waiting() bool { return len(s.Permissions)+len(s.Questions) > 0 }
func (s *State) DisplayStatus() string {
	if s.Waiting() {
		return "WAITING"
	}
	return s.Status
}

func (s *State) DisplaySince() time.Time {
	if s.Waiting() {
		return s.WaitingSince
	}
	return s.Since
}

func (s *State) UpdateRequests(permissions []api.Permission, questions []api.Question, now time.Time) bool {
	was := s.Waiting()
	s.Permissions = permissions
	s.Questions = questions
	if !was && s.Waiting() {
		s.WaitingSince = now
		return true
	}
	return false
}

func (s *State) WaitingText() string {
	var lines []string
	for _, p := range s.Permissions {
		lines = append(lines, "PERMISSION: "+p.Permission+" · "+strings.Join(p.Patterns, ", "))
	}
	for _, q := range s.Questions {
		for _, info := range q.Questions {
			text := "QUESTION: " + info.Header + " " + info.Question
			for _, o := range info.Options {
				text += " · " + o.Label + ": " + o.Description
			}
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}

// ApplyRequest accepts both current and v2 SSE spellings; unknown events are inert.
func (s *State) ApplyRequest(e api.Event, session string, now time.Time) bool {
	var p struct {
		ID        string   `json:"id"`
		SessionID string   `json:"sessionID"`
		RequestID string   `json:"requestID"`
		Action    string   `json:"action"`
		Resources []string `json:"resources"`
	}
	if json.Unmarshal(e.Properties, &p) != nil || p.SessionID != session {
		return false
	}
	permissions := append([]api.Permission(nil), s.Permissions...)
	questions := append([]api.Question(nil), s.Questions...)
	switch e.Type {
	case "permission.asked", "permission.v2.asked":
		var request api.Permission
		if json.Unmarshal(e.Properties, &request) != nil || request.ID == "" {
			return false
		}
		if request.Permission == "" {
			request.Permission = p.Action
			request.Patterns = p.Resources
		}
		permissions = upsertPermission(permissions, request)
	case "question.asked", "question.v2.asked":
		var request api.Question
		if json.Unmarshal(e.Properties, &request) != nil || request.ID == "" {
			return false
		}
		questions = upsertQuestion(questions, request)
	case "permission.replied", "permission.v2.replied":
		permissions = slices.DeleteFunc(permissions, func(v api.Permission) bool { return v.ID == p.RequestID })
	case "question.replied", "question.rejected", "question.v2.replied", "question.v2.rejected":
		questions = slices.DeleteFunc(questions, func(v api.Question) bool { return v.ID == p.RequestID })
	default:
		return false
	}
	return s.UpdateRequests(permissions, questions, now)
}

func upsertPermission(requests []api.Permission, request api.Permission) []api.Permission {
	for i := range requests {
		if requests[i].ID == request.ID {
			requests[i] = request
			return requests
		}
	}
	return append(requests, request)
}

func upsertQuestion(requests []api.Question, request api.Question) []api.Question {
	for i := range requests {
		if requests[i].ID == request.ID {
			requests[i] = request
			return requests
		}
	}
	return append(requests, request)
}
