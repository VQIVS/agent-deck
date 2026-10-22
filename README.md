# agent-deck

A keyboard-and-mouse terminal dashboard for coordinating local OpenCode sessions and teams.

```text
agent-deck · WAITING 1 · streams 2/2 · http://127.0.0.1:4096
[Sessions] [Refresh] [Notifications] [Help] [<] [>]
╭─ BUSY · Coordinator ─────────────╮╭─ WAITING · Reviewer ─────────────╮
│ Reviewing the implementation…   ││ Permission: edit · *.go          │
│ ▸ bash  go test ./...           ││ [Approve once] [Always] [Reject] │
│ main · clean                   ││ review · +2 files                │
╰────────────────────────────────╯╰──────────────────────────────────╯
```

```text
             ╭─ Teams ───────────────────────────╮
             │ > Example · 3 members              │
             │   1 busy   1 idle   1 waiting      │
             │                                    │
             │ [New team] [Import] [Open] [Export] │
             │ [Edit] [Duplicate] [All sessions]   │
             ╰────────────────────────────────────╯
```

## Features

- Directory-scoped discovery, HTTP snapshots and automatically reconnecting SSE streams.
- Paged panes, focus and zoom, fuzzy session selection, keyboard navigation and mouse controls.
- Markdown transcripts, expandable tool cards, raw details, search and optional reasoning.
- Waiting permission/question indicators, permission replies and optional desktop notifications.
- Persistent teams, member forms, model/agent selection and owned Git worktrees.
- YAML/JSON imports and exports, portable templates, previews and revision backups.
- Auto/dark/light themes, read-only mode and optional Git metadata.

## Install

Requires macOS/Linux, **OpenCode ≥1.19.0** (tested with 1.19.0), and Git for worktrees
and repository footers. Go is only needed for source installs.

```sh
brew install opencode
opencode auth login
opencode auth list && opencode models
```

Once published, the Homebrew formula `brew install qivs/tap/agent-deck` installs
OpenCode and Git dependencies. See the [installation guide](docs/install.md) for
official OpenCode alternatives, release verification, setup and troubleshooting.

With the Go version listed in [`go.mod`](go.mod):

```sh
go install github.com/qivs/agent-deck@latest
```

Release archives for macOS and Linux (amd64/arm64) are distributed through
[GitHub Releases](https://github.com/qivs/agent-deck/releases). Once a release is available,
download the matching archive, verify it against `checksums.txt`, extract it, and put
`agent-deck` on your `PATH`. No release is implied by an unreleased changelog entry.

## Quick start

Start your local OpenCode server, then launch the team picker:

```sh
opencode serve --hostname 127.0.0.1 --port 4096
agent-deck
```

```sh
agent-deck run --all                  # discover active sessions
agent-deck --read-only --all          # observe without mutations
agent-deck --theme light
agent-deck --snapshot                 # read-only HTTP snapshot
agent-deck discover --out deck.yaml
agent-deck --config deck.yaml
agent-deck new --dir /path/to/project --title 'Review changes'
agent-deck --version
```

`opencode` must be on `PATH` for attach. An HTTP server is sufficient for observation.

## Teams

Choose **New team** to set a repository, base branch, shared rules and worker capacity.
Members use an existing session, a shared directory, or a managed worktree. One member
coordinates as master; workers can receive individual briefs and prompts.

```sh
agent-deck --team example
```

Team files live under `~/.config/agent-deck/teams` (or `$XDG_CONFIG_HOME/agent-deck/teams`).
Edits are checked against the loaded revision and written atomically with backups.
See [teams](docs/teams.md) for lifecycle behavior, forms and import formats.

## Import and export

```sh
agent-deck import examples/team.yaml --name example --repo /path/to/project
agent-deck export example --out example.yaml
agent-deck export example --portable --format json --out template.json
```

Imports accept legacy decks, full team exports and portable templates. The UI can also
import selected live sessions. Preview before saving; replacement requires confirmation.
Portable exports remove session IDs, local paths and worktree ownership, while retaining
team instructions and explicit creation policies. Review prompt text before sharing.

## Permissions

Adopted sessions use **inherited from session**, with no permission rules invented or stored.
Explicit `reviewer`, `coder` and `full` presets apply **only to newly created sessions**.
Selecting a preset for an adopted member records future creation policy; it does not change
the session's live permissions. An inherited portable member needs an explicit policy before
creating a new session.

Permission replies are scoped to the owning directory. “Always” requires confirmation;
questions are displayed in the pane and answered through OpenCode attach.

## Keys and mouse

Press `?` for in-app help. Forms and dialogs show their own shortcuts.

| Key | Action |
| --- | --- |
| `Tab`, `Shift+Tab`, arrows | Focus panes across pages |
| `Enter`, `Esc` | Zoom / return |
| `p` | Fuzzy session picker; Space toggles a session |
| `m`, `Ctrl+S` | Compose / send a message |
| `n`, `s`, `a` | Nudge, confirm abort, attach |
| `y`, `Y`, `x` | Approve once, confirm always, reject |
| `/`, `j`, `k`, `v`, `R` | Search, select tool cards, raw details, reasoning |
| `PgUp`, `PgDn`, `End` | Scroll / follow tail |
| `r`, `N`, `q` | Refresh, toggle notifications, quit |

Click panes to focus, buttons to act, and tool cards to expand. The mouse wheel scrolls;
hover highlights clickable controls. Picker rows, forms and confirmation dialogs are clickable.

## Configuration

Server precedence: `--server`, `AGENT_DECK_SERVER`, deck file, then `http://127.0.0.1:4096`.
Use `--no-git` to skip repository inspection and `--notify=false` to disable macOS notifications.
`--theme` accepts `auto`, `dark`, or `light`. `--config` accepts YAML or JSON legacy decks;
managed teams use `--team`. See [`examples/team.yaml`](examples/team.yaml).

## Security model

Agent-deck normally talks only to a local OpenCode server and never stores authentication
secrets. It does not contact a hosted coordination service. Explicit server configuration
can point elsewhere; that endpoint is trusted with session operations. Keep the server
bound to loopback unless you provide your own secure access controls.

Team files store prompts, paths and session bindings. Read-only mode blocks session changes,
permission replies, attach, team edits, resource operations, and UI file-writing transfers.
Remote terminal controls are stripped before rendering. See [security](SECURITY.md) for
reporting and trust boundaries.

## FAQ

**Why is a session missing?** Discovery initially selects active sessions. Use `p` to select
idle sessions. OpenCode status, requests and streams are scoped to each session directory.

**Does changing a preset update an existing session?** No. Live rules remain owned by OpenCode.

**Can I share a team file?** Prefer `--portable`, then inspect retained prompts and instructions.

**Does it work offline?** The UI reconnects after server outages; live operations require OpenCode.

**Can I run it without a team?** Yes: `agent-deck --all` or a legacy `--config` file.

## Development

See [contributing](CONTRIBUTING.md), [architecture](docs/architecture.md), and
[changelog](CHANGELOG.md). Licensed under [MIT](LICENSE), copyright AmirMahdi Qiasvand.
