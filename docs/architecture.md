# Architecture

Agent-deck is one executable with flat, internal Go packages. There is no service layer
hierarchy and no plugin runtime.

```text
OpenCode server → opencode (HTTP + SSE) → state → ui (Bubble Tea)
                             ↑                    ↓ commands
                             └────────────────────┘
cli → config / team → ui
               ↓
          Git worktrees + local team files
```

| Package | Responsibility |
| --- | --- |
| `cli` | Flags, subcommands, process exit status and boundary errors |
| `opencode` | Directory-scoped requests, discovery and reconnecting SSE |
| `server` | Detached loopback supervisor, ownership lock, health and bounded crash restart |
| `config` | Legacy deck parsing and validation |
| `team` | Managed configuration, atomic persistence, backups and member/worktree lifecycle |
| `state` | Session snapshots, incremental events and waiting-request state |
| `transcript` | Sanitized Markdown and tool-card rendering |
| `gitinfo` | Read-only local Git metadata |
| `ui` | Dashboard, pickers, forms, dialogs, themes and input handling |

`main.go` only obtains build information and delegates to the CLI. The UI event loop owns
its mutable display state. I/O runs in commands and streams; results return as messages.
Every session request and SSE stream is scoped to its directory. Reconnection refreshes
snapshots, and pending events are replayed after a snapshot to avoid losing updates.

The team manager validates configuration, checks read-only policy and capacity, manages
owned worktrees, and creates sessions with explicit creation permissions. Adopted sessions
retain their live permissions. Persistence compares the expected file revision before an
atomic rename and keeps prior revisions in backups. These checks detect stale local edits;
they are not a cross-process database transaction.
