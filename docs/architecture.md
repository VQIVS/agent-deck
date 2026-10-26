# Architecture

## Team channel foundation

`internal/team/scheduler*.go` provides a pure task-board and scheduling policy core:
dependency/lease contention checks, capacity/cooldown proposals, ask-by-default
approval and read-only guards. It performs no session creation or retirement; a runtime
controller must revalidate each proposal and persist/journal accepted transitions.

`internal/channel` maintains a private append-only, fsynced JSONL journal with crash-tail
recovery and exclusive locking. `internal/mcp` exposes authenticated loopback HTTP tools
for roster, channel reads/posts and DMs. Identity comes from bearer credentials; tool
arguments cannot select a caller identity. Shared-directory credential isolation is not
supported and credentials must only be issued after isolation is verified.

Mention/DM delivery persists intent, attempt and acknowledgment events. Busy members stay
queued; idle delivery uses an injected transport preserving member prompt settings.
Delivery is at-least-once across crashes, with stable IDs for deduplication. Read-only,
mute, rate, size and hop guards apply. Runtime/dashboard exposure is a separate integration.

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
