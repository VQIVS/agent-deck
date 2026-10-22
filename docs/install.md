# Installation

## Prerequisites

- macOS or Linux and a modern terminal.
- **OpenCode 1.19.0 or newer**; tested with **1.19.0**. This is the oldest version
  verified against the directory-scoped session/status/event APIs, pending permission
  and question endpoints, creation-time rules, and provider/model catalog used here.
  Earlier versions may work but are not claimed as supported. OpenCode 2 compatibility
  has not been established; check `agent-deck doctor` after upgrades.
- Git for managed worktrees and repository footers.
- Go only when installing from source (see the required version in `go.mod`).

## Install OpenCode

These methods are listed in the [official installation guide](https://opencode.ai/docs/):

```sh
brew install opencode
```

Homebrew core provides OpenCode (1.19.0 on the tested machine). The official guide
recommends `brew install anomalyco/tap/opencode` for faster upstream updates.
Other officially documented options:

```sh
curl -fsSL https://opencode.ai/install | bash
npm install -g opencode-ai
```

Choose one installation method; the commands install the same executable. Review the
official script before running it if your environment requires that.

Configure a model provider using the [official CLI](https://opencode.ai/docs/cli/):

```sh
opencode auth login
opencode auth list && opencode models
```

The second line lists authenticated providers and available model identifiers. For private
providers, configure their endpoint and credentials in OpenCode, not agent-deck.

## Install agent-deck

### Homebrew

After the first formula is published to the tap:

```sh
brew install qivs/tap/agent-deck
```

The formula declares `opencode` and `git` dependencies. The tap configuration in this
repository does not itself imply that a release has already been published.

### Release binary

Download a matching macOS/Linux amd64/arm64 archive and `checksums.txt` from
[Releases](https://github.com/qivs/agent-deck/releases). Verify before extracting:

```sh
sha256sum --check --ignore-missing checksums.txt
tar -xzf agent-deck_<version>_<os>_<arch>.tar.gz
install -m 755 agent-deck "$HOME/.local/bin/agent-deck"
```

Create `$HOME/.local/bin` first if needed, and include it on `PATH`. On macOS without
`sha256sum`, compare `shasum -a 256 <archive>` against its exact line in `checksums.txt`
before extracting. A checksum mismatch must stop installation.

### Go install

```sh
go install github.com/qivs/agent-deck@latest
```

Include `$(go env GOPATH)/bin` on `PATH` (or your `GOBIN`).

### From source

```sh
git clone https://github.com/qivs/agent-deck.git
cd agent-deck
make install
```

## First run

Start the local server in one terminal, then run agent-deck in another:

```sh
opencode serve --hostname 127.0.0.1 --port 4096
```

```sh
agent-deck doctor
agent-deck
```

Create a team from the picker or import a deck. Server lifecycle (`agent-deck up`)
and repository setup (`agent-deck init`) are planned commands, not available in this
version. Until implemented, use the explicit server command and team forms above.

## Troubleshooting

| Problem | Fix |
| --- | --- |
| Server not running | `opencode serve --hostname 127.0.0.1 --port 4096` |
| Port in use | Check the existing local server, or use `--port 4097` and `agent-deck --server http://127.0.0.1:4097` |
| Permission prompts | Review the request and approve/reject; inherited session rules remain owned by OpenCode |
| No models | `opencode auth login`, then `opencode models`; restart the server after changing provider configuration |
| Old OpenCode | `brew upgrade opencode` for Homebrew core, or `opencode upgrade` for the official script install |
| Private provider unreachable | Connect the required VPN; verify DNS and provider endpoint from the same shell as the server |
| Config directory unwritable | Check ownership of `$XDG_CONFIG_HOME/agent-deck` or `~/.config/agent-deck`; rerun `agent-deck doctor` |

Agent-deck does not install OpenCode automatically or store provider credentials.
