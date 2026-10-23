package team

import (
	"fmt"
	"strings"
	"time"
)

// NudgePolicy is an opt-in, bounded policy persisted with a member's local configuration.
type NudgePolicy struct {
	Enabled     bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	MaxNudges   int           `yaml:"max_nudges,omitempty" json:"max_nudges,omitempty"`
	MinInterval time.Duration `yaml:"min_interval,omitempty" json:"min_interval,omitempty"`
	Message     string        `yaml:"message,omitempty" json:"message,omitempty"`
}

// NudgeState records successful automatic prompts and the member's done boundary.
type NudgeState struct {
	Count int       `yaml:"count,omitempty" json:"count,omitempty"`
	Last  time.Time `yaml:"last,omitempty" json:"last,omitempty"`
	Done  bool      `yaml:"done,omitempty" json:"done,omitempty"`
}

func (p NudgePolicy) Validate() error {
	if p.MaxNudges < 0 || p.MinInterval < 0 {
		return fmt.Errorf("nudge limits must not be negative")
	}
	if p.Enabled && (p.MaxNudges == 0 || p.MinInterval == 0 || strings.TrimSpace(p.Message) == "") {
		return fmt.Errorf("enabled nudges need a positive maximum, interval and message")
	}
	return nil
}

// Due only permits idle members: a stalled busy turn must not receive another prompt.
func (p NudgePolicy) Due(now, idleSince time.Time, status string, readOnly bool, s NudgeState) bool {
	if !p.Enabled || readOnly || s.Done || status != "IDLE" || p.Validate() != nil ||
		s.Count < 0 || s.Count >= p.MaxNudges || idleSince.IsZero() {
		return false
	}
	return now.Sub(idleSince) >= p.MinInterval && (s.Last.IsZero() || now.Sub(s.Last) >= p.MinInterval)
}

// Sent must be called only after prompt submission succeeds, then saved before another evaluation.
func (s *NudgeState) Sent(now time.Time) {
	s.Count++
	s.Last = now
}
