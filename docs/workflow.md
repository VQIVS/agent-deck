# Configurable team workflows

Each team can define ordered board stages with a name, color and execution category.
Use **Team → Workflow** (`W`) to edit stages and transition rules, or configure the
team file. See [the schema and CLI examples](teams.md) for the persisted format.

## Categories and moves

Categories are `todo`, `ready`, `active`, `review`, `done`, `shipped`, `blocked` and
`cancelled`. Names and colors describe the board; categories govern scheduling,
briefs, leases, review and completion. Automatic destinations use the first ordered
stage in the required category. Custom transitions restrict moves without replacing
the engine's actor and evidence checks.

The compatibility default preserves existing stages and adds `released` in the
`shipped` category. Existing Done tasks are not automatically marked released.
Accepted Done and shipped work satisfy ordinary task dependencies. Shipment is
terminal and preserves the accepted review and original completion timestamp.

The default done-to-released transition allows `release-manager` or the user and
requires a nonempty version; a release URL is optional. A lead role alone does not
grant shipment authority. Accepted work appears in the pending-release queue;
shipment sends the lead an attributed notice including the version.

## Editor

In the editor, use `j/k` to select, `n` to add, `e` to edit name/color/category,
`J/K` to reorder, `d` to remove and `t` to edit transition sources/actors.
`Ctrl+S` saves and `Esc` cancels. Mouse controls provide equivalent actions.
Occupied-stage renames preserve task placement; removing an occupied stage or
changing its category is rejected. Invalid and stale saves keep the draft and show
feedback. Reload before retrying a stale edit.

Team and board saves use coordinated revision checks. If team persistence succeeds
but board persistence fails, the explicit partial-save error requires reload and
retry; do not assume both stores were updated or overwrite newer revisions.

On the board, `m` opens configured move destinations. Selecting a shipped destination
requests the version and optional URL before the guarded move.

## Board compatibility

Boards use version 2. Version-1 boards are rejected with:
`board v1 unsupported; open it once with agent-deck v0.3.x`.
Opening a version-2 board preserves its stored workflow and does not rewrite the file.
Obsolete task `worker`, `report`, `measured` and `legacy_completion` fields and policy
`token_budget`/`cost_budget` fields are ignored when decoding older version-2 files.
Assignments use `assignee`; accepted completion requires a valid review or structured
report. Planner estimates remain available, while scaling is bounded by worker capacity,
cooldown and approval policy.
