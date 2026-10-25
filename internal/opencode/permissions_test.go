package opencode

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestPermissionAppendDecision(t *testing.T) {
	want := []Rule{
		{Permission: "*", Pattern: "*", Action: "ask"},
		{Permission: "edit", Pattern: "*", Action: "deny"},
		{Permission: "edit", Pattern: "owned/**", Action: "allow"},
	}
	for _, tc := range []struct {
		name    string
		current []Rule
		append  bool
		error   bool
	}{
		{name: "empty", append: true},
		{name: "exact", current: want},
		{name: "suffix", current: append([]Rule{{Permission: "bash", Pattern: "*", Action: "allow"}}, want...)},
		{name: "later override", current: append(slices.Clone(want),
			Rule{Permission: "edit", Pattern: "*", Action: "allow"}), append: true},
		{name: "at limit", current: make([]Rule, MaxNativePermissionRules-len(want)), append: true},
		{name: "exceeds limit", current: make([]Rule, MaxNativePermissionRules), error: true},
		{name: "over limit matching", current: append(make([]Rule, MaxNativePermissionRules), want...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/session/ses_policy" || r.URL.Query().Get("directory") != "/repo" {
					t.Errorf("unexpected native read: %s %s", r.Method, r.URL)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "ses_policy", "permission": tc.current})
			}))
			defer server.Close()
			needed, err := New(server.URL, false).permissionAppendNeeded(t.Context(), "/repo", "ses_policy", want)
			if (err != nil) != tc.error || needed != tc.append {
				t.Fatalf("needed=%v err=%v", needed, err)
			}
			if tc.error && (!strings.Contains(err.Error(), "current 8192 + incoming 3") ||
				!strings.Contains(err.Error(), "pending")) {
				t.Fatal("limit error lacks counts or pending remediation", err)
			}
		})
	}
}

func TestPermissionAppendReconciliation(t *testing.T) {
	var native []Rule
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ses_policy", "permission": native})
			return
		}
		var body struct{ Permission []Rule }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		patches++
		native = append(native, body.Permission...)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	policy := func(lease string) []Rule {
		return []Rule{
			{Permission: "*", Pattern: "*", Action: "ask"},
			{Permission: "edit", Pattern: "*", Action: "deny"},
			{Permission: "edit", Pattern: "manual/*", Action: "allow"},
			{Permission: "edit", Pattern: lease, Action: "allow"},
		}
	}
	apply := func(rules []Rule) {
		t.Helper()
		// Recreate clients to model retry/restart without an in-memory fingerprint.
		client := New(server.URL, false)
		if err := client.ApplyPermissions(t.Context(), "/repo", "ses_policy", rules); err != nil {
			t.Fatal(err)
		}
	}
	access := func(path string) string {
		for i := len(native) - 1; i >= 0; i-- {
			rule := native[i]
			// Native '*' crosses path separators. These fixtures use only literals and '*'.
			pattern := "^" + strings.ReplaceAll(regexp.QuoteMeta(rule.Pattern), `\*`, ".*") + "$"
			matches := regexp.MustCompile(pattern).MatchString(path)
			if (rule.Permission == "*" || rule.Permission == "edit") && (rule.Pattern == "*" || matches) {
				return rule.Action
			}
		}
		return "ask"
	}
	for range 300 {
		apply(policy("old/*"))
	}
	if patches != 1 || len(native) != 4 {
		t.Fatalf("unchanged retries grew native policy: patches=%d rules=%d", patches, len(native))
	}
	apply(policy("new/*"))
	if access("old/nested/file") != "deny" || access("new/nested/file") != "allow" ||
		access("manual/nested/file") != "allow" {
		t.Fatal("changed lease lost revoke/manual semantics", native)
	}
	apply(policy("old/*"))
	if patches != 3 || len(native) != 12 || access("new/nested/file") != "deny" ||
		access("old/nested/file") != "allow" {
		t.Fatal("reverted lease incorrectly skipped historical policy", native)
	}
}

func TestPermissionReadFailures(t *testing.T) {
	for _, body := range []string{`[]`, `{}`, `{"id":"other"}`, `{"id":"ses_policy","permission":{}}`, `invalid`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			_, err := New(server.URL, false).permissionAppendNeeded(t.Context(), "/repo", "ses_policy",
				[]Rule{{Permission: "*", Pattern: "*", Action: "ask"}})
			if err == nil {
				t.Fatal("invalid native policy accepted")
			}
		})
	}
}

func TestPermissionReadUnavailableAndReadOnly(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	rules := []Rule{{Permission: "*", Pattern: "*", Action: "ask"}}
	if _, err := New(server.URL, true).permissionAppendNeeded(t.Context(), "/repo", "ses_policy", rules); err == nil {
		t.Fatal("read-only apply accepted")
	}
	if requests != 0 {
		t.Fatal("read-only apply performed native request")
	}
	if _, err := New(server.URL, false).permissionAppendNeeded(t.Context(), "/repo", "ses_policy", rules); err == nil {
		t.Fatal("unavailable native policy read accepted")
	}
	if requests != 1 {
		t.Fatal("unavailable native read fell back to append")
	}
}

func TestPermissionAppendAmbiguousResponseRetry(t *testing.T) {
	var native []Rule
	patches := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "ses_retry", "permission": native})
			return
		}
		var body struct{ Permission []Rule }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		native = append(native, body.Permission...)
		patches++
		// Model a proxy error after the backend persisted the append.
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	rules := []Rule{{Permission: "*", Pattern: "*", Action: "ask"}}
	client := New(server.URL, false)
	if err := client.ApplyPermissions(t.Context(), "/repo", "ses_retry", rules); err == nil {
		t.Fatal("ambiguous write unexpectedly succeeded")
	}
	if err := New(server.URL, false).ApplyPermissions(t.Context(), "/repo", "ses_retry", rules); err != nil {
		t.Fatal(err)
	}
	if patches != 1 || !slices.Equal(native, rules) {
		t.Fatal("ambiguous retry appended twice", native)
	}
}
