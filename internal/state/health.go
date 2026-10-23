package state

import "time"

const DefaultStallAfter = 5 * time.Minute

type messageInfo struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	Role      string `json:"role"`
	Time      struct {
		Created int64 `json:"created"`
	} `json:"time"`
	Tokens struct {
		Input int64 `json:"input"`
		Cache struct {
			Read int64 `json:"read"`
		} `json:"cache"`
	} `json:"tokens"`
}

func NewWithClock(clock func() time.Time, stallAfter time.Duration) State {
	if clock == nil {
		clock = time.Now
	}
	return State{Status: "UNKNOWN", Since: clock(), Seen: map[string]bool{}, clock: clock, StallAfter: stallAfter}
}

func (s *State) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func (s *State) ActivitySince() time.Time {
	if s.LastActivity.After(s.Since) {
		return s.LastActivity
	}
	return s.Since
}

func (s *State) Stalled(now time.Time) bool {
	return s.Status == "BUSY" && !s.Waiting() && s.StallAfter > 0 &&
		!s.ActivitySince().IsZero() && now.Sub(s.ActivitySince()) >= s.StallAfter
}

func (s *State) observeContext(id, role string, created, tokens int64) {
	if role != "assistant" || created < s.contextTime ||
		(created == s.contextTime && id != s.contextID && id < s.contextID) {
		return
	}
	s.contextID, s.contextTime, s.ContextTokens = id, created, max(0, tokens)
}
