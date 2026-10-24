# Isolated pool lifecycle verification

`scripts/pool_verify.py` starts the actual dashboard and its production MCP/runtime
inside a PTY. A deterministic loopback fake supplies OpenCode responses; scripted
actors use authenticated MCP tools to create, inspect, progress and review tasks.
No paid model is involved and the fixture does not claim software implementation.

## Run

Build the current checkout into a temporary binary, then pass its absolute path:

```sh
go build -o /tmp/agent-deck-pool-verify .
# User/lead prepares a disposable empty fixture once:
git init /tmp/agent-deck-pool-scratch
python3 scripts/pool_verify.py --binary /tmp/agent-deck-pool-verify --repo /tmp/agent-deck-pool-scratch --timeout 90 --output /tmp/pool-evidence
python3 scripts/pool_verify_test.py
```

The default is a strict lifecycle **and offline-recovery** gate. A failure exits
nonzero with the persisted board, lifecycle effects, scratch team bindings and PTY
tail. `--skip-offline` runs only the baseline for diagnosis; its success does not
satisfy the offline-recovery gate.

## Assertions

- A uniquely named scratch team starts with no workers and three independent ready
  tasks. Auto policy permits two workers and a two-second scaling cooldown.
- The real scheduler creates workers and installs private control identities. Each
  new worker calls `my_tasks` using its generated credential.
- The fake rejects the first assignment prompt with HTTP 503. The real delivery
  queue retries it and the harness waits for accepted assignment text.
- Worker MCP calls tick the criterion and request review. The authenticated master
  accepts each review. The PTY must render the actual persisted task title.
- The final task's worker session is removed from the fake's scoped inventory.
  Strict mode requires a fresh control/session and durable offline handoff before
  continuing. Its task must remain recoverable and ultimately complete.
- After accepted completion, auto reconciliation retires idle pool workers until
  only the master remains.

## Isolation and cleanup

Each run uses a new temporary HOME, XDG config/state roots and random team name,
with a caller-provisioned disposable Git fixture passed through `--repo`.
It never loads the user's team directory,
connects to the live OpenCode server, runs Git mutation commands or writes repository
metadata. The user/lead owns fixture preparation and removal.

The fake binds an ephemeral loopback port. The PTY child is terminated and reaped
in `finally`; the HTTP server and temporary directory are removed on success and
failure. `--output` retains only lifecycle assertions and PTY text, not credentials,
private OpenCode configs or raw MCP requests. The output directory is caller-owned.

## Verification status

Strict default verified on 2026-10-24 after the T-19 production recovery fix:
two workers created, three tasks reviewed done, new-worker board access,
failed-brief retry, confirmed-offline recovery with a fresh control/session, and
scale-down to zero workers all passed against a freshly built binary. The strict
run used `--timeout 90`, without `--skip-offline`. Protocol unittest also passed.

The harness previously exposed a production recovery failure: missing-session
observation worked and a recovery action was desired, but automatic execution did
not recover the worker. T-19 corrected the production path and received independent
QA acceptance. The strict default assertions were retained throughout; baseline
skip-mode success was never used to waive the recovery requirement.

The retained success JSON includes `offline_recovered=true`, an
`offline-recovered` capture with `fresh_control=true`, `completed=3`,
`scale_up_workers=2`, `scale_down_workers=0` and
`failed_brief_recovered=true`, plus PTY output. A separate independent QA review
of this harness and capture is still required for T-11 acceptance.

Full repository checks and commit remain the master's integration gate once all
workers are idle; this worker runs only the isolated demo/protocol checks.
