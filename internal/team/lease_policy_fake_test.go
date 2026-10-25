package team

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	api "github.com/vqivs/agent-deck/internal/opencode"
)

// appendPolicyFake retains native permission history while preserving each test's
// existing PATCH assertions and failure injection. Only successful PATCHes append.
func appendPolicyFake(t *testing.T, next http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	var mu sync.Mutex
	history := map[string][]api.Rule{}
	return func(w http.ResponseWriter, r *http.Request) {
		session := strings.TrimPrefix(r.URL.Path, "/session/")
		escaped := strings.TrimPrefix(r.URL.EscapedPath(), "/session/")
		if session == r.URL.Path || session == "status" || strings.Contains(escaped, "/") {
			next(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]any{
				"id": strings.TrimPrefix(r.URL.Path, "/session/"), "permission": history[r.URL.Path],
			}); err != nil {
				t.Error(err)
			}
			return
		}
		if r.Method != http.MethodPatch {
			next(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		response := &policyFakeResponse{ResponseWriter: w, status: http.StatusOK}
		next(response, r)
		if response.status >= 200 && response.status < 300 {
			var patch struct{ Permission []api.Rule }
			if err := json.Unmarshal(body, &patch); err != nil {
				t.Error(err)
				return
			}
			history[r.URL.Path] = append(history[r.URL.Path], patch.Permission...)
		}
	}
}

type policyFakeResponse struct {
	http.ResponseWriter
	status int
}

func (w *policyFakeResponse) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
