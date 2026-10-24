# Changelog

All notable changes are documented here, following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Version numbers follow [Semantic Versioning](https://semver.org/).

## [0.2.7] - 2026-10-24

### Fixed

- The master is notified when the user creates, edits, assigns or moves a task.
- Release workers can publish releases from temporary release worktrees.
- Agent-deck control-command permission rules no longer block repository or temporary worktree paths.
- Release workers can edit `CHANGELOG.md` inside temporary release worktrees.
- Release changelog edit permissions stay root-independent in OpenCode control sessions.

## [0.2.6] - 2026-10-24

### Added

- `release` permission preset for a release worker: commits its leased files, tags, fast-forwards,
  pushes and runs goreleaser, but cannot rewrite history or disturb the shared checkout.

## [0.2.4] - 2026-10-24

### Fixed

- Planned tasks default to parallel, so tasks with non-overlapping paths run at the same time;
  `parallel: false` marks a serial task.

## [0.2.3] - 2026-10-24

### Fixed

- Workflow identities created by a dashboard are recognised by the supervisor (and the reverse):
  the control root no longer depends on the process working directory.

## [0.2.2] - 2026-10-24

### Fixed

- Workflow identities keep their team tools across runtime restarts: the runtime reinstalls the
  credentials and registers them with the live OpenCode instance.
- Search tools (`grep`, `glob`, `list`) follow the member's `read` permission and todo tools are
  allowed, so members are not prompted for read-only work.

## [0.2.1] - 2026-10-24

### Changed

- Workers' permission requests go to the master as notices; the master decides them with the new
  `answer_request` tool. Only the master's own requests reach the user.
- Members can use their own team's agent-deck tools without permission prompts.
- Roster changes are recorded in the channel without waking idle members.

## [0.2.0] - 2026-10-24

### Added

- Workflow task model: `backlog → ready → in_progress → review → done`, plus `blocked` and
  `cancelled`; acceptance checklists, owned paths, dependencies, priority, labels, assignee and
  reviewer, attributed history and comments, human-readable estimates (`"45m"`).
- Master-driven board: MCP tools `create_task`, `update_task`, `split_task`, `set_assignee`,
  `move_task`, `comment_task`, `review_task`, `list_tasks`; worker tools `my_tasks` and
  `update_my_task`. Authority comes from the team roster, never from tool arguments.
- agent-deck starts assigned ready tasks when the worker is free, applies its file leases and
  sends a brief generated from the task; one active task per worker.
- Board notices as private messages from agent-deck: review requests to the reviewer, requested
  changes to the assignee, done/blocked/cancelled to the master, bounded comment wake-ups.
- Board operating manuals in master and worker system prompts when the member has team tools.
- Automatic board pass in the team runtime: assignments always; worker creation and retirement
  only in `auto` mode; it never approves.
- Shared-checkout workflow identity: per-member control sessions with their own MCP credentials,
  explicit and confirmed, with a generated handoff and a reset action.
- Kanban task manager (`b`): bordered cards, colored states, member colors, progress, filters,
  swimlanes by assignee, detail panel, task forms, validated moves, user review.
- Review stage with a team definition of done; accept or request changes with a comment.
- Observed Git facts per task (branch, commits ahead, commits touching the task's paths), kept
  separate from worker claims.
- `agent-deck task ls|show|add|edit|move|assign|comment|review` for scripts and terminals.

### Changed

- Member names `agent-deck` and `user` are reserved.
- Board storage moved to version 2; v0.1 boards migrate once with a backup.

### Security

- Team tools, leases and approvals remain cooperative guards; same-user agents with unrestricted
  shell access can bypass them. Member-written comments are quoted and labelled in notices.

## [0.1.0] - 2026-10-24

### Added

- Detached, loopback-only server lifecycle (`up`, `down`, `status`) with crash restart,
  private state/logs, and an explicit interactive startup offer.
- Directory-scoped OpenCode HTTP discovery and reconnecting SSE streams.
- Paged terminal dashboard with keyboard and mouse controls, search, Markdown transcripts,
  expandable tool cards, and permission request handling.
- Managed teams, worktree lifecycle, member forms, portable import/export, and local backups.
- Auto, dark, and light themes; read-only mode; optional Git metadata and desktop notifications.
- Contributor tooling, CI, and cross-platform release configuration.
- Headless team control: `agent-deck team ls|status|send|wait|last|requests|approve|reject`.
- Shared-checkout worker pools with file leases enforced through native session permission rules.
- Session health: STALLED state, context gauge, opt-in auto-nudge, and confirmed compaction.
- `agent-deck init`, role templates, a portable project file, a literal safe-command allowlist, and
  extended `doctor` checks.
- Team channel over a loopback MCP server: authenticated members, DMs, mentions that wake idle
  members, hop/rate/size limits, muting, and a Channel pane.
- Structured worker reports with DONE cards and an Inbox across the team.
- Pool scheduling: work board, master tools to propose, assign, and retire workers behind an approval
  policy, and recovery for interrupted actions.
- Stepped forms, a command palette, adaptive pane layout, and a waiting-requests counter.

### Security

- Approvals, leases, and Git write denies are cooperative guards; agents with unrestricted shell
  access run as the user and can bypass them. See the security notes in `docs/teams.md`.
