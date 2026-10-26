package team

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/vqivs/agent-deck/internal/gitinfo"
)

func git(ctx context.Context, dir string, args ...string) (string, error) {
	c := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	c.Env = gitinfo.Environment()
	b, e := c.CombinedOutput()
	if e != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(b)), e)
	}
	return strings.TrimSpace(string(b)), nil
}
