package server

import (
	"fmt"
	"os"
	"path/filepath"
)

func StateRoot() (string, error) {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve state home: %w", err)
		}
		root = filepath.Join(home, ".local", "state")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("XDG_STATE_HOME must be absolute")
	}
	return filepath.Join(root, "agent-deck"), nil
}
