package mcp

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

func runtimeStateDirectory(options *RuntimeOptions) error {
	if options.StateDir == "" {
		root := os.Getenv("XDG_STATE_HOME")
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve runtime state home: %w", err)
			}
			root = filepath.Join(home, ".local", "state")
		}
		options.StateDir = filepath.Join(root, "agent-deck", "runtime")
	}
	if err := os.MkdirAll(options.StateDir, 0o700); err != nil {
		return fmt.Errorf("create runtime state: %w", err)
	}
	info, err := os.Lstat(options.StateDir)
	if err != nil {
		return fmt.Errorf("inspect runtime state: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("runtime state must be a real directory")
	}
	if err := os.Chmod(options.StateDir, 0o700); err != nil {
		return fmt.Errorf("protect runtime state: %w", err)
	}
	return nil
}

func runtimeStatePath(root, key, suffix string) string {
	canonical, err := filepath.Abs(key)
	if err == nil {
		key = canonical
	}
	return filepath.Join(root, fmt.Sprintf("%x.%s", sha256.Sum256([]byte(key)), suffix))
}

func discoveryPath(options *RuntimeOptions) string {
	return runtimeStatePath(options.StateDir, options.JournalPath, "runtime.json")
}
