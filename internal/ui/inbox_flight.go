package ui

import "sync"

type inboxRequestKey struct{ directory, session, request string }

// InboxFlights serializes pending replies across inbox and pane controls.
type InboxFlights struct {
	mu      sync.Mutex
	pending map[inboxRequestKey]bool
}

func (m *Model) permissionFlights() *InboxFlights {
	if m.inbox.Flights == nil {
		m.inbox.Flights = &InboxFlights{}
	}
	return m.inbox.Flights
}

func (f *InboxFlights) Begin(directory, session, request string) bool {
	if directory == "" || session == "" || request == "" {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending == nil {
		f.pending = make(map[inboxRequestKey]bool)
	}
	key := inboxRequestKey{directory, session, request}
	if f.pending[key] {
		return false
	}
	f.pending[key] = true
	return true
}

// Finish releases failed attempts; successful attempts stay blocked until Observe sees removal.
func (f *InboxFlights) Finish(directory, session, request string, err error) {
	if err == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.pending, inboxRequestKey{directory, session, request})
}

func (f *InboxFlights) Observe(directory, session string, pendingIDs []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	current := make(map[string]bool, len(pendingIDs))
	for _, id := range pendingIDs {
		current[id] = true
	}
	for key := range f.pending {
		if key.directory == directory && key.session == session && !current[key.request] {
			delete(f.pending, key)
		}
	}
}
