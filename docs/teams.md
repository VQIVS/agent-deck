# Managed teams: design and operational contract

## Quick worker setup

Choose **+ Worker** to open a short form: name, model, brief, workspace, directory and
optional existing session ID. Models load automatically; **Select model** (`Ctrl+P`)
opens the model list, including a team/OpenCode default option. Press `Enter` on a model
or workspace field to open its choices, and on a text field to advance.
Models come from the directory-scoped configured-provider catalog, not the global list.
Type in the picker to search, use arrows to navigate and Enter to confirm. Backspace edits
the search, `Ctrl+U` clears it, and Esc returns without changing the selection.

Workers default to a new session in the team's shared repository directory with the
coder creation policy. Leave the session ID empty and click **Create worker** (`Ctrl+S`).
Agent-deck saves the member, creates the session, and persists its returned ID and directory
in the managed team file. If creation fails, the member remains saved for retry using **Start**.
Choose `worktree` when you want an isolated branch instead. To adopt a session, enter its
ID and directory; live permission rules stay unchanged. An empty adopt ID creates a new
shared session instead. **Advanced** (`Ctrl+A`) reveals the remaining member settings.

## Entry points and ownership

Default startup opens a team picker backed by
`~/.config/agent-deck/teams/*.yaml` (`XDG_CONFIG_HOME` is honored).
`--team NAME` opens a saved team; `--all` opens ad-hoc discovery. Explicit
`--config` and `--snapshot` retain their existing behavior. The picker exposes
New, Open, Edit, Duplicate, Delete (confirmed), and All sessions. Opening a team
only observes its recorded sessions; it never silently starts or aborts anything.
Status summaries use directory-scoped HTTP status/permission/question reads.

## Headless commands

`agent-deck team` offers `ls`, `status`, `send`, `wait`, `last`, `requests`, `approve`, and
`reject`. Put flags after the subcommand. Every command supports `--json`, `--read-only`,
and `--server URL`; all except `ls` require `--team NAME`. `ls --team NAME` filters the list.
Server precedence is the explicit flag, `AGENT_DECK_SERVER`, then the saved team server.

- `status`: name, role, session, directory, idle/busy/waiting/offline state, pending request
  count, latest assistant input + cache-read tokens, and last session activity. JSON activity
  is Unix milliseconds; unknown is zero. Missing/unstarted sessions appear offline. A failed
  member read appears offline with an error field while the other members are still reported.
- `send --member NAME (--text TEXT | --file PATH)`: uses the same `team.Options` prompt
  settings as the dashboard; refuses an unstarted member. Records a private pre-send assistant
  baseline under `teams/.sent/` for a subsequent wait. Delivery errors retain that baseline
  because an HTTP failure can leave submission uncertain. The team YAML is not changed.
- `wait --member NAME [--timeout 30m] [--poll 5s]`: polls until idle (exit 0), permission or
  question waiting (exit 2), or timeout (exit 3). Errors exit 1 with details on stderr.
  After a CLI send it first requires busy, or a new completed assistant message, so an initial
  idle status cannot masquerade as completion. A missing busy transition with no completed
  reply times out rather than reporting success. Without a send baseline, an already-idle
  member gets a grace period of two polls, capped at ten seconds. Read-only wait does not
  modify receipts. Receipts are scoped to server, directory and session; simultaneous send/wait
  callers for the same member should serialize their operations.
- `last --member NAME`: latest assistant text, without tool or reasoning parts. Empty text
  means no text in the latest assistant message. Reads use the server's latest 30 messages.
- `requests`: pending permission/question IDs with member, directory and request details;
  requests from other sessions in the same directory are excluded.
- `approve --id ID [--always]`: defaults to once. `reject --id ID [--message TEXT]` rejects.
  Both resolve the request against this team's pending requests and use its owning directory.
  Questions are listed for visibility; answer them through OpenCode attach. An ambiguous or
  stale request ID is an error. Read-only blocks send and permission replies.

All observations and actions use each member's own directory query. None of these commands
starts sessions, changes creation permissions, or creates worktrees.

## Data model and editing

A versioned YAML team contains name, server, repository, base, default model,
capacity, shared rules, roster template, and members. Exactly one member is
master. Members record name, role, model, OpenCode agent, inline system prompt or
prompt file, brief, five permission settings, workspace mode, branch template,
directory, branch, session ID, and whether the worktree was created by this app.
Models are `provider/model`; providers/models and agents are fetched from the
server. Form fields are reachable by Tab and mouse; multiline text editors are
used for prompts, rules, briefs, and roster templates. No hand editing is needed.

Workspace modes: `worktree` creates a separate worktree from base with branch
template `{team}/{member}`; `shared` uses the chosen directory; `adopt` records an
existing session without creating a worktree. The UI permits adoption explicitly;
adopted sessions are subject to the same read-only guard as other members.
Permission profiles enumerate read/edit/bash/webfetch/external_directory with
allow/ask/deny. Presets: reviewer (read-only), coder (edits allowed, shell asks),
full (all allowed). Profile changes take effect on newly created sessions.

## API integration

The inspected server supports `POST /session` with a permission ruleset and
`POST /session/{id}/prompt_async` with `system`, `agent`, and model
`{providerID,modelID}`. Team rules and the member system prompt are supplied via
the real per-prompt `system` field, including subsequent dashboard messages.
They are not merely prepended to the brief. Prompt-file contents are read when
sending a prompt and never copied into another member. Roster messages are sent
to the master's recorded directory with its own system/model/agent settings.
Requests always carry `?directory=`. Older servers rejecting these supported
fields produce an explicit error; there is no silent fallback to weaker rules.

## Explicit live operations

The dashboard offers Team settings, + Worker, Remove, and Change master.
An unstarted member can be started through the member UI. Creating a worker is
an explicit action: validate capacity and configuration; create its worktree if
requested; create the session with permissions; persist the session ID; send its
brief with system/rules; send the master an updated roster. Failures preserve
recorded resource IDs so recovery does not require guessing what was created.
Capacity counts recorded, started workers; the master does not consume a slot.
Capacity overflow is blocked rather than queued.

Remove always confirms. It aborts only the selected session and sends the master
a removal roster. App-owned worktrees are removed only when their branch is an
ancestor of base, unless the user explicitly selects “delete worktree anyway.”
Dirty worktrees require that same force opt-in. Branch refs are retained. Shared
and adopted directories are never deleted. Removing a team file does not abort
its sessions or remove its worktrees. Change master selects a recorded member,
demotes the previous master, persists the roles, and sends the new master a roster.
All mutations, including saves, creation, adoption, removal, and roster sends,
are disabled by `--read-only`.

## Persistence and concurrent edits

Every configuration change is written with restrictive permissions to a temporary
file in the destination directory, synced, then atomically renamed. The five
previous versions are retained in a sibling history directory. Before overwrite,
the store detects changed on-disk content and refuses to clobber it. The UI
checks the team file periodically and reloads valid external edits; invalid edits
leave the running configuration intact and show an error. Changes to recorded
session identity cause dashboard reload rather than reusing stale pane state.
Team names resolve to safe filename slugs; filesystem paths cannot escape the
team store. Server URLs with embedded credentials/query credentials are rejected.
No API keys, tokens, or provider credentials are stored. User-authored prompts
are plain text; do not put secrets in prompts or rules.

## Import/export and presentation

The picker exposes Import and per-team Export. The equivalent commands are
`agent-deck import FILE [--name NAME] [--repo DIR]` and
`agent-deck export TEAM [--out FILE] [--format yaml|json] [--portable]`.
Both YAML and JSON exports use a versioned team inside an `agent-deck/team`
envelope. Non-portable exports preserve every team/member field.

Legacy decks in `sessions` or `master`/`workers` form are converted locally:
lead/master maps to master, all other members become workers, existing session
IDs/directories are adopted, and ownership is false. There must be exactly one
master and unique member names. Adopted members use capability `inherited` with
no recorded permission rules, shown as “inherited from session” in UI/exports.
Explicit reviewer/coder/full presets apply only to newly created sessions;
they do not describe or change an adopted session's effective permissions.
An inherited member cannot Start a new session until an explicit creation policy
is chosen. Invalid capability names and inherited profiles with recorded rules
are rejected. Saving an adopted member's policy does not send a roster.

The local OpenCode `/doc` also advertises `PATCH /session/{sessionID}` with a
`permission: PermissionRuleset` body (directory-scoped). Agent-deck does not use
that endpoint for preset edits or import: all policy edits remain creation-only,
and live permission rules are left untouched.

Live import uses read-only HTTP discovery, lets the user select individual
sessions, choose the master, and name the team. File/live imports show the entire
proposed configuration before saving; replacement previews include before/after.
The confirmation uses the previewed disk version, so intervening changes fail
with a conflict. Import never calls Start, Send, Roster, or worktree operations.
CLI `--yes` can accept a new team's preview; existing teams always confirm.
Existing export files require confirmation too. Read-only UI mode blocks writes.

Portable export removes repo, directory, session, bound branch, ownership and
absolute system-file/branch-template bindings. Adopt becomes shared with no
session. Inline prompts, models, agents, capabilities, permissions, roles, rules
and roster templates survive. Absolute prompt files are not read or embedded.
Import requires a new local repository binding and validates the resulting team;
shared members use that repo, worktree members remain unstarted. Portable export
does not redact arbitrary absolute-path strings inside user-authored prompt text.

Theme tokens live in `internal/ui/theme.go`: auto/dark/light palettes cover the
picker, dashboard, forms and dialogs; Glamour uses the same dark/light selection.
The picker centers a wordmark and rounded list panel, shows colored status chips
and server last-update times, and anchors keyboard/mouse hints in a footer.
Names load before network summaries. Status reads always use each member's
directory and never mutate live sessions. Unknown activity is labeled unavailable.

## Roster template

Go `text/template` receives team name, base, reason, master and workers. The
default includes worker names, IDs, dirs, branches, briefs, and directory-scoped
HTTP instructions for status, permissions, questions, and message history.
Templates are editable in Team settings and validated before saving.

## Verification

Unit tests cover round-trip validation, atomic saves/conflict/history behavior,
roster rendering, capacity, and safe branch/path generation. Temporary git repos
exercise worktree creation and merged/unmerged/dirty removal. Fake servers cover
session creation, prompt system/permissions, abort, roster delivery, and read-only
blocking. Mouse/form tests cover zone routing and disabled mutation controls.
Live sessions not created by the test remain read-only.
