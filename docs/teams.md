# Managed teams: design and operational contract

## Workflow task model (v0.2)

### Task CLI parity

Task commands act as the privileged local user without requiring a TTY. All task changes
use the same `team.ChangeTasks` transition/history/notice boundary as the board UI.

```sh
agent-deck task ls --team example --json
agent-deck task show --team example --id T-1 --json
agent-deck task add --team example --title 'Implement parser' \
  --criteria 'Parser handles valid input' --criteria 'Regression tests pass' \
  --path 'internal/parser/**' --estimate 45m --priority high
agent-deck task edit --team example --id T-1 --description 'Handle the new input format'
agent-deck task assign --team example --id T-1 --assignee Worker --reviewer Master
agent-deck task move --team example --id T-1 --state ready
agent-deck task comment --team example --id T-1 --text 'Clarified acceptance criteria'
agent-deck task review --team example --id T-1 --decision changes --comment 'Cover empty input'
```

`add`/`edit` accept `--file PATH` containing a TaskSpec JSON object; explicitly supplied
flags override file fields. Repeat `--criteria`, `--path`, `--dependency` and `--label`.
Durations use values such as `10m` or `2h`. Assignee/reviewer names must be current members.
All commands support `--read-only`, `--server` and `--json`; read-only rejects mutations.
List/show are observation-only and do not start a runtime or migrate files on disk.

After a durable mutation, notices are sent to an already-running authenticated runtime
owner. The CLI never starts a runtime merely to notify. If none is available, the task
change still succeeds and reports **notices skipped**; they are not claimed queued or
automatically replayed. JSON mutation output includes a separate optional `warning`.
These local-user privileges remain cooperative under the same-user bash trust model.

The compatibility workflow uses `backlog → ready → in_progress → review → done`, with `blocked`
and `cancelled` alternatives and a separate `released` shipment stage. Tasks carry descriptions, acceptance checklists, owned
paths/dependencies, priority/labels, assignee/reviewer, Git links, timestamps, attributed
history and comments. JSON estimates accept human durations such as `"10m"`; legacy
numeric nanoseconds remain readable.

Only ready tasks schedule. Review requires a reviewer; done requires an accepted review
of the current task content or a passing structured report against its exact acceptance
criteria. Checklist checks are progress claims, not verification. Workers may progress,
comment on or block their own assignments; task planning belongs to the master/user.

Writable legacy loads migrate once under the board lock with an exact-byte backup;
read-only loads only migrate in memory. Legacy done outcomes retain an explicit legacy
completion marker so dependents remain schedulable; failed outcomes become blocked,
and pending tasks with unmet dependencies return to backlog. Legacy markers are not new
accepted reviews. History/comment budgets and bounded backup retention prevent unbounded growth.
### Configured workflow stages and shipment

A workflow separates stage names from execution categories: `todo`, `ready`, `active`,
`review`, `done`, `shipped`, `blocked` and `cancelled`. A team can call its review stage
`Testing` or its done stage `Ready for release`; the category controls scheduling,
evidence checks, notices and completion accounting. Stage names never grant authority.

Task move inputs use the configured stage name, including names containing spaces.
Use the same stage names in MCP `move_task.state` and CLI `task move --state`.
Shipment also requires a nonempty version; optional release evidence is separate:

```sh
agent-deck task move --team example --id T-1 --state released --version v1.2.3
agent-deck task workflow --team example --id T-1 --json
agent-deck task pending-release --team example --json
```

The default done → released move belongs to `release-manager` or the privileged user;
lead/master status alone does not authorize shipment. Custom transition rules remain
subject to acceptance evidence and actor guards. A release records a delivery claim,
not independently verified deployment. Shipped work is terminal, satisfies ordinary
dependencies, and preserves the implementation completion evidence and timestamp.
It does not create another completion-metrics sample or reacquire implementation leases.

`task workflow` and MCP `task_workflow` expose the persisted ordered definition and,
when an ID is supplied, moves permitted by the engine for the caller. A shipped move
in the preview still requires an actual version when executed. Read-only callers can
inspect definitions and the pending-release queue; they cannot mutate tasks. The CLI
acts as the privileged local user; MCP derives member authority from authentication.
MCP `pending_release` and CLI `pending-release` list accepted done-category work, not
already shipped tasks. Shipment metadata appears as `release_version`/`release_url`.
Entering the done category queues a private notice for a present `release-manager`;
entering shipped queues a version-attributed notice for the lead. Event-keyed runtime
journal delivery is idempotent across repeated notification attempts and restart.
The pending-release query remains available even if the release-manager was absent
when acceptance occurred. Renaming a stage within the same category does not create
another acceptance/shipment event.

MCP shipment example: `move_task` with
`{"id":"T-1","state":"released","version":"v1.2.3","release_url":"https://example.test/releases/v1.2.3"}`.
The URL is optional; the version is mandatory. Unknown stages, denied transitions,
missing acceptance evidence and unauthorized releases fail without persisting a move.

## Shared-checkout workflow identity (v0.2)

## Kanban task manager (v0.2)

### Review evidence and team definition of done

Team settings include **Definition of done (YAML criteria)**, a strict list of `id`, `text`
and `required` entries. Detail views show the merged task/team requirements separately
from claimed checklist progress. Conflicting criterion IDs/text are rejected. Task reports
must match the merged definition and current task/member/session binding. A reviewer or
privileged user may explicitly accept or request changes with a comment; acceptance is a
manual review decision, not an independently observed passing test result. Changes return
the task to in-progress and notify the assignee through the common workflow notice path.

Opening detail collects read-only Git facts for the assignee's code checkout: branch,
ahead count and up to 100 commits touching task paths. Observations have timestamps and
are kept separate from worker-reported branch/commit/test claims. Results captured for
old checkout/base/path identities are discarded; a changing HEAD invalidates collection.
Git facts do not establish test success or acceptance completion.

### Reproducible scratch workflow demo

Run `make build`, then `python3 scripts/demo_workflow.py --output /absolute/capture/path`.
The harness creates a real temporary Git repository, scratch team/private control
identities, a fake loopback OpenCode endpoint and a real dashboard PTY. One fixed goal
drives a scripted fake master through real MCP tools; automatic assignment applies leases,
the worker requests review, and an independent reviewer accepts. Captures include backlog,
ready, in-progress, review and done PTY frames plus persisted state/history/notice evidence.
This verifies integration, not autonomous model planning or passing implementation tests.
No existing team or live OpenCode session is used; scratch resources are removed afterward.

**Board (`b`)** opens a visual Kanban with bordered cards and colored state headers.
Selected cards use a thick accent border. Member badges use the same stable color as
session headers and member rows; cards show priority, checklist progress, labels, age and
dependency/blocked tags. Assigned ready work shows **waiting for member** until agent-deck
starts it. At 80 columns, navigate horizontally instead of compressing every state.

Arrows/hjkl navigate, Enter or double-click opens the detail panel, PgUp/PgDown scrolls
detail, and Esc returns. `f` cycles assignee, `t` label, `z` state, `/` searches, `0` clears
filters, and `s` groups by assignee. `n` creates, `e` edits, `a` assigns, `c` comments,
`m` then 1–7 requests a state move, and `w` opens the assignee's current session pane.
Card `[<]`/`[>]` buttons also request validated moves. Task forms use Tab/Shift+Tab,
Ctrl+J newline and Ctrl+S save. `v` opens user review (accept or changes with a comment).
`p` switches to the policy approval view.

Every UI task edit uses `team.ChangeTasks` as privileged user and forwards its notices
through the runtime owner. A one-active-task conflict or invalid dependency/state move
appears as a board/form error without changing the stored task. Successful saves remain
successful if notice delivery fails, and that separate delivery error is shown. Read-only
allows navigation/filtering/detail but blocks mutations. Member rows and pane headers
show each member's active task.

Shared members can use distinct authenticated team tools through private per-member
control directories. **Team → Workflow identity** is an explicit confirmed setup action:
the member must be idle (or unstarted) and have an explicit permission preset. It creates
a new OpenCode session, retaining the previous session and directory as transcript references;
it does not transfer the old model context or modify the code checkout. Existing worktree
members continue to use **Team tools**.
The warning explicitly states that model conversation context is lost. Setup sends a
generated first-prompt handoff containing the last report/assistant text and open tasks.
**Reset identity** explicitly restores the retained prior binding and revokes control
credentials, retaining both transcript references. Removing a member deletes its control
session; owned config and empty validated control directories are cleaned on revocation.

`dir` remains the code/Git checkout; `control_dir` is the session/config/API directory.
All status, messages, permissions and prompts use the session directory. Native edit leases
are rebased for the control session's non-Git root, while external access is limited to the
shared checkout and private-root denies remain last. Prompts identify the checkout and
require absolute code paths/workdir. Credential setup/rotation is private to the control
directory; no token is written to the shared repo or portable export.

OpenCode's inspected API has directory-scoped MCP configuration, not a per-session MCP
override. Separate temporary control configs were verified to send distinct bearer headers;
a throwaway control session also edited an allowed shared-repo file with rebased edit rules.
This provides attributable cooperative identity. Same-user unrestricted bash can still read
other credentials or bypass guards; it is not sandbox isolation. Setup is never implicit
for adopted sessions, and read-only blocks setup.
Each control directory has a random per-member parent rather than an enumerable sibling
member layout. Native absolute peer reads were denied in a temporary non-Git-session probe;
the relative-shell attempt timed out and is not claimed verified. Directory permissions
and random names are accidental-access hardening, not isolation from the same OS user.

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
Every member works in the team's checkout. To adopt a session, enter its
ID and directory; live permission rules stay unchanged. An empty adopt ID creates a new
shared session instead. **Advanced** (`Ctrl+A`) reveals the remaining member settings.

## Entry points and ownership

Default startup opens a team picker backed by
`~/.config/agent-deck/teams/*.yaml` (`XDG_CONFIG_HOME` is honored).
`--team NAME` opens a saved team; `--all` opens ad-hoc discovery. Legacy deck-file
flags `--config` and `--snapshot` have been removed. The picker exposes
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

Workspace modes: `shared` uses the chosen directory; `adopt` records an existing
session. Worktree members are no longer supported: a team file with `mode: worktree`
fails to load with a message asking for `mode: shared`. The UI permits adoption explicitly;
adopted sessions are subject to the same read-only guard as other members.
Permission profiles enumerate read/edit/bash/webfetch/external_directory with
allow/ask/deny. Presets: reviewer (read-only), coder (edits allowed, shell asks),
full (all allowed). Profile changes take effect on newly created sessions.

## Release workers

The `release` permission preset is for a worker that cuts releases from the shared checkout. It
may commit its own leased files (usually `CHANGELOG.md`), tag, fast-forward and push, create
temporary worktrees and run `goreleaser`/`gh release`. It may not force-push, delete branches or
tags, reset, rebase, check out, switch, stash, restore or clean, so it never rewrites history or
disturbs the checkout other members are editing. Like every guard here this is cooperative.

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
Private roots use absolute `external_directory` denies (including resolved XDG paths)
to block both native reads and edits outside the worktree. Edit patterns themselves are
worktree-relative; absolute private-path edit rules are not relied upon.

## Project setup

`agent-deck init --dir /path/to/repo --template bugfix-tester` detects the Git root and
base branch, then saves a private team without creating sessions. `--list-templates`
lists built-ins and user overrides. Existing teams are never overwritten.
`--team`, `--base`, `--server` and `--model` override detected/default values.

`agent-deck doctor --team NAME [--dir DIR]` adds repository/base, prompt-file and
unapplied-lease checks to server/model diagnostics.
The dashboard no longer automatically approves exact-command allowlists. Permission requests
remain available for explicit approval or rejection in the selected pane and Inbox.
Read-only dashboards cannot answer requests.

## Role templates

Built-in templates are `upgrade-team`, `review-pair`,
`bugfix-tester` and `custom`. User YAML templates under the config directory replace an
entire built-in by name. Advanced worker forms offer **Role template** choices; selecting
one applies its system prompt and creation capability on save, plus agent/model when set.
An existing brief is retained. Unknown fields and multiple documents are rejected.

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

For a member with a workflow identity, **Team → Team tools** confirms installation of a
private MCP entry into its control directory's `opencode.json`. The file must already be Git-ignored; JSON is merged
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

`Ctrl+P` is unbound on dashboard/workspace screens; the command palette has been removed.
Member forms retain their contextual `Ctrl+P` model picker. Help (`?`) lists the
retained keyboard actions. Inbox (`i`), Channel (`c`) and Board (`b`) have
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
workers, cooldown, role models and approval mode (`ask`, `auto`, `off`). Through
agent-deck's guarded interfaces only the privileged user can configure policy or approve
scaling; these are cooperative controls, bypassable by unrestricted same-user bash.
Repository files cannot raise policy through the supported merge path.

Authenticated masters get `propose_workers`, `assign_task` and `retire_worker` MCP tools.
Each action is rechecked against current team revision, task dependencies, idle workers,
lease contention and capacity before lifecycle effects. Native worker edit/Git
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
Pending permissions/questions always win. The dashboard uses the default five-minute
threshold; member stall controls and automatic nudges have been removed. Press `n`
to nudge the selected member manually. Read-only blocks nudges.

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
an explicit action: validate capacity and configuration; create the session with permissions; persist the session ID; send its
brief with system/rules; send the master an updated roster. Failures preserve
recorded resource IDs so recovery does not require guessing what was created.
Capacity counts recorded, started workers; the master does not consume a slot.
Capacity overflow is blocked rather than queued.

Remove always confirms. It aborts only the selected session and sends the master
a removal roster. Shared and adopted directories are never deleted. Removing a team
file does not abort its sessions. Change master selects a recorded member,
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

Team transfers are available through the CLI; import/export and live-import screens
have been removed from the picker. Use
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

Use **All sessions** or `--all` to monitor live sessions without importing them.
Team creation and explicit adoption remain available through member forms.
CLI imports preview the proposed configuration before saving; replacement previews
include before/after and reject intervening changes. Import never starts sessions or
sends messages. CLI `--yes` can accept a new team's preview; existing teams always
confirm. Existing export files require confirmation too.

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
roster rendering and capacity. Fake servers cover
session creation, prompt system/permissions, abort, roster delivery, and read-only
blocking. Mouse/form tests cover zone routing and disabled mutation controls.
Live sessions not created by the test remain read-only.
