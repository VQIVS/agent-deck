package cli

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

func offerServer(ctx context.Context, address string, readOnly bool) bool {
	if readOnly || !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	u, err := url.Parse(address)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || u.Scheme != "http" {
		return false
	}
	checks := newDiagnostics(address)
	version, err := checks.command(ctx, "opencode", "--version")
	if err != nil || !supportedVersion(string(version)) || checks.health(ctx) == nil {
		return false
	}
	fmt.Print("Start a managed local OpenCode server? [y/N] ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil || !strings.EqualFold(strings.TrimSpace(line), "y") {
		return false
	}
	port := u.Port()
	if port == "" {
		port = "80"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return false
	}
	if err := serverCommand("up", []string{"--port", port}); err != nil {
		fmt.Fprintln(os.Stderr, "Start server:", err)
		return false
	}
	return true
}
