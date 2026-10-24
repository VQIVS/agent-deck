package mcp

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestRuntimeAttachmentSharesOwnerAndSurvivesDetach(t *testing.T) {
	f := newRuntimeFixture(t)
	owner := startTestRuntime(t, f)
	connection, err := OpenRuntime(t.Context(), &RuntimeOptions{
		Provider: f.provider, JournalPath: owner.options.JournalPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if connection.owner != nil || connection.Endpoint() != owner.Endpoint() {
		t.Fatal("did not attach")
	}
	if err := connection.PostUser(t.Context(), "attached message", []string{"bob"}); err != nil {
		t.Fatal(err)
	}
	records, err := connection.ReadUser(t.Context())
	if err != nil || records[len(records)-1].Text != "attached message" {
		t.Fatalf("attachment read: %v, %v", records, err)
	}
	if err := connection.InstallCredential(t.Context(), "bob"); err == nil {
		t.Fatal("attachment installed shared credentials")
	}
	connection.Close()
	if _, err := owner.ReadUser(t.Context()); err != nil {
		t.Fatal("detach closed owner", err)
	}
	f.mu.Lock()
	f.manager.ReadOnly = true
	f.mu.Unlock()
	readOnly, err := OpenRuntime(t.Context(), &RuntimeOptions{
		Provider: f.provider, JournalPath: owner.options.JournalPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	if err := readOnly.PostUser(t.Context(), "blocked", nil); err == nil {
		t.Fatal("read-only attachment mutated owner")
	}
}

func TestRuntimeOwnerEndpointRejectsWrongCredentialAndDiscoveryPermissions(t *testing.T) {
	f := newRuntimeFixture(t)
	owner := startTestRuntime(t, f)
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost,
		strings.TrimSuffix(owner.Endpoint(), "/mcp")+"/user", strings.NewReader(`{"action":"read"}`))
	request.Header.Set("Authorization", "Bearer forged")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatal("forged owner token accepted")
	}
	path := discoveryPath(&owner.options)
	os.Chmod(path, 0o644)
	if _, err := readDiscovery(path); err == nil {
		t.Fatal("public discovery accepted")
	}
	os.Chmod(path, 0o600)
}

func TestRuntimeGitClearsHookEnvironment(t *testing.T) {
	t.Setenv("GIT_INDEX_FILE", "/tmp/wrong-index")
	t.Setenv("GIT_DIR", "/tmp/wrong-repo")
	command := runtimeGit(t.Context(), t.TempDir(), "status")
	for _, entry := range command.Env {
		if strings.HasPrefix(entry, "GIT_INDEX_FILE=") || strings.HasPrefix(entry, "GIT_DIR=") {
			t.Fatal("hook git environment leaked", entry)
		}
	}
}
