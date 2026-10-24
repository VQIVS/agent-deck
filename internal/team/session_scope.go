package team

import (
	"fmt"
	"path/filepath"
)

func (m *Member) validateSessionScope() error {
	if err := m.Nudge.Validate(); err != nil {
		return err
	}
	if m.StallAfter != nil && *m.StallAfter < 0 {
		return fmt.Errorf("stall threshold must not be negative")
	}
	if m.ControlDir != "" && (!filepath.IsAbs(m.ControlDir) || filepath.Clean(m.ControlDir) == filepath.Clean(m.Dir)) {
		return fmt.Errorf("control directory must be absolute and distinct from code checkout")
	}
	return nil
}

// SessionDir is the API/config scope, distinct from the code checkout for workflow identities.
func (m *Member) SessionDir() string {
	if m.ControlDir != "" {
		return m.ControlDir
	}
	return m.Dir
}
