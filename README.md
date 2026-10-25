# agent-deck

**Your agents. One deck. You at the helm.**

A keyboard-and-mouse terminal dashboard for coordinating local OpenCode sessions and teams.

*agent-deck is built by agent-deck: a master and a worker pool, steered from its own dashboard.*

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

### Workspace screens

Teams can customize their board stages, colors and transitions in **Team → Workflow** (`W`).
Automation follows stage categories, so renamed stages retain scheduling and review semantics.
The default includes **Released** after accepted Done work; recording shipment requires a version
and an authorized release-manager or user. See [workflow configuration](docs/workflow.md).

- **Tabs (`1`–`6`, or `Alt+1`–`Alt+6`)**: Dashboard, Git, Summary, Channel, Board and Inbox.
  Click the tab row to switch screens. In Board/Inbox use Alt-number to preserve their numeric controls;
  shortcuts are suspended while typing in forms or composing messages.
- **Git (`2`)**: changed-file sidebar beside a colored diff, with staged and unstaged sections.
  `j/k` select files; `Tab` changes focus, `h/l` focus files/diff, `gg/G` jump first/last,
  `Ctrl+d/u` page the diff, and `r` refreshes. Click a file or use the wheel to scroll its diff.
  `v`/`e` opens the selected file in **Vim** (must be on `PATH`); exiting Vim refreshes the preview.
  Read-only mode opens Vim nonmodifiable without swap/viminfo writes. `--no-git` disables Git reads.
- **Summary (`3`)**: chronological task history, comments, worker reports and available channel records,
  plus clearly labeled, unverified assistant excerpts. `Tab` filters members; `j/k`, `gg/G` and paging
  navigate; `r` refreshes evidence. Persisted records cover available team history; session excerpts
  use the latest 30 loaded messages, so earlier transcript content may be missing.
- **Inbox (`i`)**: waiting requests, errors, stalls and report cards; select an entry to act on its exact session.
- **Channel (`4` or `c`)**: attributed message cards with roles, recipients, private labels and timestamps.
  Click **Compose** or press `Enter`/`m`; `Enter` sends, `Ctrl+J` adds a newline, and `Tab` completes
  `@member` mentions. `Esc` preserves a draft; failed sends keep it too. `j/k`, `gg/G` and the wheel
  browse history; `o` expands/collapses roster updates. Explicit `@member` mentions queue delivery.
- **Board (`b`)**: tasks and policy-gated pool proposals with detailed confirmation.
- **Commands (`Ctrl+P`)**: searchable actions with availability/read-only explanations.
  Help (`?`) is generated from the same command registry.

Worker forms show explicit field steps. Pane controls wrap to available width, and the
header counts waiting sessions separately from permission/question requests.

**Security model:** approvals, leases and Git denies are cooperative guards against mistakes
and runaway loops. A session with unrestricted bash runs as your OS user and can bypass
these controls, including by calling APIs or changing files directly. Real isolation needs
an OS sandbox; agent-deck does not provide one yet.

- Directory-scoped discovery, HTTP snapshots and automatically reconnecting SSE streams.
- Paged panes, focus and zoom, fuzzy session selection, keyboard navigation and mouse controls.
- Markdown transcripts, expandable tool cards, raw details, search and optional reasoning.
- Waiting permission/question indicators, permission replies and optional desktop notifications.
- Stalled-session badges, model context gauges, confirmed compaction and opt-in bounded idle nudges.
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

`brew install --cask vqivs/tap/agent-deck` installs agent-deck with its OpenCode and Git
dependencies. See the [installation guide](docs/install.md) for
official OpenCode alternatives, release verification, setup and troubleshooting.

With the Go version listed in [`go.mod`](go.mod):

```sh
go install github.com/vqivs/agent-deck@latest
```

Release archives for macOS and Linux (amd64/arm64) are distributed through
[GitHub Releases](https://github.com/vqivs/agent-deck/releases). Once a release is available,
download the matching archive, verify it against `checksums.txt`, extract it, and put
`agent-deck` on your `PATH`. No release is implied by an unreleased changelog entry.

## Quick start

Start your local OpenCode server, then launch the team picker:

```sh
agent-deck up
agent-deck
```

For a new project, use `agent-deck init --template bugfix-tester` to save a team from
repository defaults or `agent-deck.yaml`; list recipes with `agent-deck init --list-templates`.
Check project bindings with `agent-deck doctor --team NAME`.

```sh
agent-deck run --all                  # discover active sessions
agent-deck --read-only --all          # observe without mutations
agent-deck --theme light
agent-deck --snapshot                 # read-only HTTP snapshot
agent-deck --team example --snapshot  # snapshot of one team
agent-deck discover --out deck.yaml
agent-deck --config deck.yaml
agent-deck new --dir /path/to/project --title 'Review changes'
agent-deck --version
```

`opencode` must be on `PATH` for attach. An HTTP server is sufficient for observation.

`agent-deck up --port 4096` starts a detached loopback-only server with crash restart
and bounded backoff. `agent-deck status` reports health and ownership; `agent-deck down`
stops only its managed server. PID metadata and logs live in `$XDG_STATE_HOME/agent-deck`
(default `~/.local/state/agent-deck`). A healthy existing server is left running as external.
When offline, interactive dashboard startup offers to start the server; read-only mode
never starts one. You can also run `opencode serve --hostname 127.0.0.1 --port 4096` yourself.

## Headless team control

The workflow board also has CLI parity: `agent-deck task ls|show|add|edit|move|assign|comment|review --team NAME`.
Changes use the same validated task/history path as the UI, with notices delivered through
an existing runtime owner or an explicit skipped-notice warning. See [task CLI details](docs/teams.md#task-cli-parity).

Drive saved teams from scripts or another agent without opening the dashboard:

```sh
agent-deck team ls
agent-deck team status --team example --json
agent-deck team send --team example --member Worker --text 'Run the package tests'
agent-deck team wait --team example --member Worker --timeout 30m --poll 5s
agent-deck team last --team example --member Worker
agent-deck team requests --team example
agent-deck team approve --team example --id per_example
agent-deck team reject --team example --id per_example --message 'Use local fixtures'
```

All commands accept `--json`, `--read-only`, and `--server`. `wait` exits **0** for idle,
**2** for a pending permission/question, and **3** on timeout; other errors exit **1**.
`send` also accepts `--file` instead of `--text` and preserves member agent/model/system
settings. See [headless command details](docs/teams.md#headless-commands) for waiting semantics.

## Teams

Choose **New team** to set a repository, base branch, shared rules and worker capacity.
Members use an existing session, a shared directory, or a managed worktree. One member
coordinates as master; workers can receive individual briefs and prompts.
Advanced **Leases** assign non-overlapping repository-relative paths to workers. New
sessions enforce edit ownership and deny worker Git writes; **Team → Apply leases**
explicitly resets an idle session's effective permissions while preserving its context.
See [lease behavior](docs/teams.md#shared-checkout-leases).

Click **+ Worker**, enter a name and brief, and use **Select model** to choose from the
server's models. New workers default to the team's directory. Leave the session field
empty and click **Create worker**: agent-deck creates the session and saves its ID in
the team configuration. Choose `worktree` for an isolated branch, or provide an existing
session ID to adopt it. **Advanced** reveals prompts, agents, roles and permission settings.
`Enter` advances a field or opens its choices; `Ctrl+P` selects a model and `Ctrl+S` saves.
The model picker lists only models from configured OpenCode providers. Type to search,
use ↑/↓ to move, and press Enter to select; `Ctrl+U` clears the search.

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
| `m`, `Enter` | Compose / send a message (`Ctrl+J` newline; `Ctrl+S` also sends) |
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
