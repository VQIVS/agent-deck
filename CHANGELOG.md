# Changelog

All notable changes are documented here, following [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Version numbers follow [Semantic Versioning](https://semver.org/).

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
