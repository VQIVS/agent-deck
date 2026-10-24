package main

import (
	"os"

	"github.com/vqivs/agent-deck/internal/cli"
)

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	os.Exit(cli.Run(os.Args, cli.Build{Version: version, Commit: commit, Date: date}))
}
