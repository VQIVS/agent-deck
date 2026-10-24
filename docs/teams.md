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
  baseline under `$XDG_STATE_HOME/agent-deck/sent/` (default `~/.local/state/agent-deck/sent/`)
  for a subsequent wait. Delivery errors retain that baseline
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

## Shared-checkout leases

Workers default to the developer's shared checkout. In **Advanced**, set **Leases** to
repository-relative globs, one per line (for example `internal/parser/**`). Ownership is
saved with the member and retained by portable export. Potentially overlapping prefixes
between members are rejected conservatively. `*` crosses directory separators in OpenCode;
`**` works too. Use specific prefixes. Lease paths cannot include parent traversal or Git
metadata. The configured repository must be the Git worktree root; a worktree member uses
its own root. Leases appear in member lists, pane headers, rosters and per-message system
instructions. Workers run package-scoped tests; the master runs full checks when they are idle.
Saving the first lease enables persistent `lease_pool` mode. Removing the final lease
does not disable it: a worker with no leases has no editable paths. Each bound session
records its applied policy fingerprint; changes to ownership, role or creation rules show
**leases PENDING apply** until that member's policy is explicitly applied. Portable exports strip
the applied fingerprint while retaining ownership and pool mode.

When any team member has leases, newly created sessions receive native edit rules:
workers deny all edits then allow their own leases; the master denies other members'
leases. Workers also receive deny rules for Git write commands, including commit/push,
reset/checkout and worktree operations. These native tool rules remain active without
the dashboard running. They are tool permissions, not an OS sandbox: arbitrary shell
programs can mutate files, so shell permission approval still needs review. Git denies
match write-command positions, including common wrappers and `-C`/`-c` options; harmless
`git diff`, `git log`, `git status` and tests of `gitinfo` remain available. This is a
cooperative guard, not a sandbox or a complete shell parser.
Any session with unrestricted bash runs as the user and can bypass approvals, leases,
Git denies and private-path rules through other programs or direct HTTP calls. These
controls prevent accidents/runaway loops, not hostile same-user agents. Real isolation
requires an OS sandbox, planned separately.

Saving a lease changes configuration only. For a live member choose **Team → Apply leases**
and confirm. The member must be idle with no pending requests and have explicit creation
permissions (select coder/reviewer/full in Advanced for an inherited member first).
OpenCode PATCH appends permission rules and later matches win. This action appends a
complete ruleset beginning with `permission: "*", pattern: "*", action: ask`, followed by
base policy, edit ownership and Git denies. The reset overrides earlier grants and denies
without losing the session's context or transcript. An unchanged fingerprint skips PATCH;
changed policies grow the native rule list but replace its effective behavior. Unknown
inherited tool permissions reset to ask, so an explicit preset is required before applying.
Apply each affected live member, including the master, after changing ownership. Existing
sessions keep their prior effective rules until explicitly applied. Read-only blocks apply.
Apply leases is available for the master too. Adopted masters do not acquire these rules
until explicitly applied; choose an explicit preset first. Private-path guards cover every
pool session, including masters, and protect configuration/state/boards cooperatively.

## Project setup

`agent-deck init --dir /path/to/repo --template bugfix-tester` detects the Git root and
base branch, then saves a private team without creating sessions. `--list-templates`
lists built-ins and user overrides. `--project FILE` selects portable policy explicitly;
otherwise a root `agent-deck.yaml` is used when present. Existing teams are never overwritten.
`--team`, `--base`, `--server` and `--model` override detected/default values.

`agent-deck doctor --team NAME [--project FILE] [--dir DIR]` adds repository/base,
project schema, prompt-file and unapplied-lease checks to server/model diagnostics.
With private team `approval: allowlist`, the dashboard approves bash requests once only when it can
match a complete tool command by call ID against `safe_commands`. Missing tool input,
abbreviated patterns, shell syntax, other permission types and read-only mode never auto-approve.
Members with editing enabled or unknown inherited edit permissions never auto-approve commands:
build/test commands execute repository code. Only explicitly edit-denied members qualify.
Tool input permits only command/description/timeout; workdir and unknown fields are refused.
Approval attempts are audited with time/session/command under the XDG state directory;
failed replies clear their retry marker.

## Portable project policy and templates

`internal/team` provides strict version-1 `agent-deck.yaml` project policy and role
templates. Project policy accepts portable roles, prompts, model, base, capacity and an
exact-command allowlist; it rejects session IDs and workspace bindings. Merging requires
explicit private local bindings. Opening a saved team merges root `agent-deck.yaml` with
its private bindings case-insensitively and keeps unmentioned members and local overrides.
The overlay remains in memory for prompts/display. Forms edit private values; saves and
session lifecycle operations use the private team, then re-merge repository policy.
Repository approval/command lists are suggestions only: approval settings come exclusively
from private configuration. Repository capabilities can only tighten explicit local tool
permissions; inherited sessions stay inherited. Repository prompt text is labeled and
subordinate to private policy. Repository rules/system text still enters prompts; opening
a repository therefore trusts its instructional content, even though it cannot raise permissions.
Doctor uses the same overlay. Live session permissions
still require explicit Apply leases.
Built-in templates are `upgrade-team`, `review-pair`,
`bugfix-tester` and `custom`. User YAML templates under the config directory replace an
entire built-in by name. Advanced worker forms offer **Role template** choices; selecting
one applies its system prompt and creation capability on save, plus agent/model when set.
An existing brief is retained. Unknown fields and multiple documents are rejected.

Safe-command matching compares complete parsed argument lists, rejecting shell operators,
substitutions, expansions and executable paths. Permission patterns alone are insufficient
evidence for automatic approval; full tool commands must be verified first. Runtime setup
and UI integration are described as they become available below.

## Structured report core

## Team channel runtime

Managed dashboards start or attach to a loopback-only team MCP runtime. `agent-deck up`
supervisors also start runtimes for lease-pool teams; both use the same private journal
and authenticated discovery file in XDG state, so only one owns it. Start the supervisor first for a
persistent owner. A dashboard-created owner closes with that dashboard; attachments do
not stop an existing owner. Read-only dashboards do not start runtime services.
The supervisor only becomes an owner, never an attachment: it retries while a dashboard
owns the journal, takes over after that owner exits, and waits for cleanup on shutdown.
Unchanged restarts seed the last journal roster instead of waking every member again.

Open **Channel** (`c`), press `m` to post as the user, and mention `@member` to queue a
wakeup. Ordinary unmentioned posts do not wake agents. Busy/waiting members queue until
idle; successful delivery is journaled and retries carry stable IDs. The user view can
see DMs; member reads remain identity-filtered. Shared/adopted members cannot receive
authenticated tools: directory credentials cannot isolate sessions sharing a directory.

For an isolated worktree, **Team → Team tools** confirms installation of a private MCP
entry into its `opencode.json`. The file must already be Git-ignored; JSON is merged
atomically, preserving unrelated configuration. Tokens rotate/revoke on binding/role
changes and owner shutdown. This is local credential separation, not a filesystem sandbox.
Roster changes broadcast through the guarded queue. Delivery is at-least-once across crashes.
Persistent membership UUIDs survive session changes and distinguish removed/re-added members;
lease-pool teams missing IDs migrate atomically with history only when their runtime starts.
Unrelated/non-pool team files are never rewritten by runtime migration. Imports discard IDs;
remove/re-add creates a new identity even in one save.
`muted: true` in private member configuration blocks posting/delivery/credentials.
Hop inheritance covers the current wake only and resets on an accepted post; escalation
to the master remains possible. Credential cleanup failures are visible and do not block
other members' delivery. Runtime config locks live in private state, never in worktrees.

The owner token permits privileged user-channel reads/posts and is stored under private
state. Credential installation/revocation requires the private owner credential and UI
confirmation; member tokens cannot invoke it. Attachments work with supervisor owners.
Recorded loopback config ownership permits reinstall after an unclean owner exit.
Worker read/bash rules deny access to agent-deck configuration/state paths cooperatively.
All processes still share the same OS user: agents with other filesystem access can defeat
these guards, so this is not a security boundary against hostile same-user code.

## Structured worker reports

The runtime registers `submit_report` for authenticated workers. Identity comes from the
credential/current session, and checks use private `definition_of_done` criteria. Required
criteria need passing test evidence with matching `criterion_id`; `not_run` does not pass.
Reports persist privately and queue a master notification. The inbox refreshes current
report cards, labeled worker-reported DONE versus inferred text and independent metadata.

The report store validates a worker's current identity/session, trusted checklist criteria,
reported checks and open issues before private atomic persistence. Worker-reported claims
and independently observed metadata are separate fields. Inferred last-message reports are
explicitly labeled and do not imply checklist completion or passing tests. CLI/MCP submission
and DONE-card integration remain separate runtime work.

## Session health

## Keyboard and mouse screens

`Ctrl+P` opens the command palette: type to search, arrows select, Enter executes, Esc
closes. Actions are revalidated against the selected session/request and read-only mode.
Help (`?`) uses the same command registry. Inbox (`i`), Channel (`c`) and Board (`b`) have
their own navigation; Esc returns to the dashboard. Team/member forms show field steps,
Previous/Next, Save/Cancel, model choices and Advanced controls. Enter advances fields,
Ctrl+J inserts a newline and Ctrl+S saves.

Pane buttons wrap as complete labels, with matching mouse targets. Header request counts
separate waiting sessions from individual permissions (`P`) and questions (`Q`). Golden
screens cover 80×24 and 160×48; `python3 scripts/smoke_ui.py` exercises team opening,
worker creation, permission approval and message composition through isolated fake HTTP
endpoints and a real PTY.

## Work board and policy-gated pool

Private work boards live under `$XDG_STATE_HOME/agent-deck/boards/<team>/`. Configure one
with `agent-deck team board --team NAME --file board.json`; inspect it with the same command
without `--file`. Missing boards default to scaling off. Board policy contains min/max
workers, cooldown, role models, budgets and approval mode (`ask`, `auto`, `off`). Only the
privileged user can configure that policy or approve scaling; repository files cannot.

Authenticated masters get `propose_workers`, `assign_task` and `retire_worker` MCP tools.
Each action is rechecked against current team revision, task dependencies, idle workers,
lease contention, capacity and budgets before lifecycle effects. Native worker edit/Git
rules are applied by the lifecycle adapter. Effects and completion are journaled with
stable action IDs; uncertain effects stop for inspection rather than being blindly repeated.
Only worker creation needs `ask` approval; `off` blocks creation but still permits existing
worker assignments. Retirement is limited to explicitly pool-created idle workers, never
user-configured/adopted members. Approval views show task, paths, role, model and reason.
Concurrent approvals merge under the board lock; completed stale-revision journal records
are compacted while uncertain intents remain recoverable.

Inspect an uncertain create with `agent-deck team recover --team NAME --id ACTION`.
Use `--decision adopt` or `--decision delete` to resolve the recorded orphan after typed
terminal confirmation. Session IDs are recorded before binding saves; action-tagged session
titles support discovery after a crash before the effect record completes. A bound session
is never deleted by orphan cleanup.

Open **Board** (`b`) to inspect tasks/proposals, `y` to approve the selected proposal or
`x` to reject it. Pending proposals also appear in Inbox (Enter opens the board) and
`agent-deck team requests`; approve/reject using `--id pool:<action-id>`. CLI approvals are
explicit privileged-user operations. Read-only blocks every board/lifecycle mutation.
Pool approvals and board-policy writes require a terminal and typing `APPROVE`; pool
session shell rules deny invoking agent-deck itself, including for masters. These remain
cooperative guards rather than same-user process isolation.
Rejected IDs remain rejected across restarts. Changing desired state produces new action IDs.

Live team reloads preserve focus, zoom and transcript state only for the same server,
directory and session. Canceled snapshot results are ignored. Team overview rows expose
status, request counts, permission policy and leases with matching three-line click targets.

Pane badges distinguish **STALLED** from **WAITING**. Busy sessions with no meaningful
part changes for five minutes become stalled; status polling does not refresh activity.
Pending permissions/questions always win. Advanced member settings expose the stall
threshold (`0s` disables) and optional auto-nudge policy: enabled, maximum, interval,
message, and task-done flag. Nudges default off, target idle sessions only, recheck live
status, and persist successful-send counts/timestamps. Done, waiting, busy and read-only
members are never nudged. A member becoming busy during the final check is a silent skip.
A real submission/save failure disables only that member's nudge policy and persists it;
the error is shown. Persistence failures are also reported so they can be resolved.

The pane shows context input + cache-read tokens against the session model's configured
context limit, or **unknown** when unavailable. Warnings begin at 75%, danger at 90%.
Choose **Compact** or press `C`, then confirm with `y`. Compaction rechecks live requests
and status and uses the model from the session's latest assistant message. Busy or waiting
sessions must first become idle (resolve the request or explicitly stop/abort, then wait).
Read-only blocks compaction. The dashboard resyncs after completion.

Open **Inbox** (`i`) for a filterable team feed of permissions, questions, errors and
stalls. Arrows select, `/` searches, Tab filters kind, Enter opens the owning pane.
Permission approvals/rejections retain the exact request and member identity across
confirmation; `a` attaches to questions and `n` nudges the selected member. Read-only
blocks mutations. Structured report adapters and historical events follow separately.

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
