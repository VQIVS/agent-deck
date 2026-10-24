package mcp

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

type configOwner struct {
	Config   string `json:"config"`
	Entry    string `json:"entry"`
	Endpoint string `json:"endpoint"`
}

func recordRuntimeConfigOwner(credential *runtimeCredential, endpoint, path string) error {
	owner := configOwner{Config: credential.config, Entry: credential.entry, Endpoint: endpoint}
	b, err := json.Marshal(owner)
	if err != nil {
		return fmt.Errorf("encode config ownership: %w", err)
	}
	return writeDiscovery(path, b)
}

func recoverableRuntimeEntry(raw json.RawMessage, credential *runtimeCredential, path string) bool {
	if !strings.HasPrefix(credential.entry, "agent_deck_") {
		return false
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var owner configOwner
	if json.Unmarshal(b, &owner) != nil || owner.Config != credential.config || owner.Entry != credential.entry {
		return false
	}
	var entry struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	if json.Unmarshal(raw, &entry) != nil || entry.Type != "remote" || entry.URL != owner.Endpoint {
		return false
	}
	return validOwnedEndpoint(entry.URL)
}

func validOwnedEndpoint(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && u.Scheme == "http" && net.ParseIP(u.Hostname()).IsLoopback() && u.Port() != "" &&
		u.Path == "/mcp" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}
