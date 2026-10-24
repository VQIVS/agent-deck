package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func loadRuntimeConfig(path string) (values map[string]json.RawMessage, original []byte, failure error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return make(map[string]json.RawMessage), nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("inspect MCP config: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("MCP config must be a regular file, not a symlink")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read MCP config: %w", err)
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(b, &config); err != nil || config == nil {
		return nil, nil, fmt.Errorf("MCP credential merge requires a valid JSON object; JSONC is not rewritten")
	}
	return config, b, nil
}

func configServers(config map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	servers := make(map[string]json.RawMessage)
	if raw, exists := config["mcp"]; exists {
		if err := json.Unmarshal(raw, &servers); err != nil || servers == nil {
			return nil, fmt.Errorf("existing MCP configuration must be an object")
		}
	}
	return servers, nil
}

func installRuntimeConfig(credential *runtimeCredential, endpoint string) error {
	if _, err := os.Lstat(filepath.Join(credential.dir, "opencode.jsonc")); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("credential installation refuses a coexisting opencode.jsonc")
	}
	return changeRuntimeConfig(credential, endpoint, false)
}

func removeRuntimeConfig(credential *runtimeCredential) error {
	return changeRuntimeConfig(credential, "", true)
}

func changeRuntimeConfig(credential *runtimeCredential, endpoint string, remove bool) error {
	lockPath := credential.config + ".agent-deck.lock"
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open credential config lock: %w", err)
	}
	lock := os.NewFile(uintptr(fd), lockPath)
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("lock credential config: %w", err)
	}
	config, original, err := loadRuntimeConfig(credential.config)
	if err != nil {
		return err
	}
	servers, err := configServers(config)
	if err != nil {
		return err
	}
	if remove {
		if !ownedRuntimeEntry(servers[credential.entry], credential.token) {
			return nil
		}
		delete(servers, credential.entry)
	} else {
		if raw, exists := servers[credential.entry]; exists && !ownedRuntimeEntry(raw, credential.token) {
			return fmt.Errorf("MCP server entry already exists; refusing to overwrite user config")
		}
		entry, err := json.Marshal(map[string]any{
			"type": "remote", "url": endpoint, "oauth": false,
			"headers": map[string]string{"Authorization": "Bearer " + credential.token},
		})
		if err != nil {
			return fmt.Errorf("encode runtime MCP entry: %w", err)
		}
		servers[credential.entry] = entry
		if _, exists := config["$schema"]; !exists {
			config["$schema"] = json.RawMessage(`"https://opencode.ai/config.json"`)
		}
	}
	encoded, err := json.Marshal(servers)
	if err != nil {
		return fmt.Errorf("encode MCP servers: %w", err)
	}
	config["mcp"] = encoded
	return writeRuntimeConfig(credential.config, config, original)
}

func ownedRuntimeEntry(raw json.RawMessage, token string) bool {
	var entry struct {
		Headers map[string]string `json:"headers"`
	}
	return len(raw) != 0 && json.Unmarshal(raw, &entry) == nil && entry.Headers["Authorization"] == "Bearer "+token
}

func writeRuntimeConfig(path string, config map[string]json.RawMessage, original []byte) error {
	b, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode MCP config: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".agent-deck-config-*")
	if err != nil {
		return fmt.Errorf("create private MCP config: %w", err)
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if err := finishRuntimeConfig(f, append(b, '\n')); err != nil {
		return err
	}
	_, current, err := loadRuntimeConfig(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return fmt.Errorf("MCP config changed during merge; retry explicitly")
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("replace MCP config: %w", err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open MCP config directory: %w", err)
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync MCP config directory: %w", err)
	}
	return nil
}

func finishRuntimeConfig(f *os.File, b []byte) error {
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return fmt.Errorf("write MCP config: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync MCP config: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close MCP config: %w", err)
	}
	return nil
}
