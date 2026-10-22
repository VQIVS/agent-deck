# Contributing

Install Go (the version in `go.mod`), golangci-lint v2 (2.12.2), and Lefthook (1.13.6).

```sh
git clone https://github.com/qivs/agent-deck.git
cd agent-deck
make setup-hooks
make check
make test-race
```

## Code conventions

Keep packages flat: at most one directory level beneath `internal/`. Tests belong next to
the code. Split UI files by screen or interaction, rather than adding subpackages.
Use gofumpt and three import groups: standard library, dependencies, this module (`make fmt`).
Keep lines within 120 characters and functions small. Explain exported types and surprising
constraints; avoid comments that narrate the code.

Pass `context.Context` first for I/O. Wrap errors with operation-specific context and `%w`;
use `errors.Is` and `errors.As`. Report errors only at CLI/UI boundaries. Avoid mutable
package state and `init`. Keep read-only checks at mutation boundaries.

Prefer isolated HTTP servers and temporary Git repositories in tests. Do not use personal
paths, real session identifiers, or running sessions in fixtures. `make smoke-live` is an
explicit, read-only check against a local OpenCode server.
After `make build`, `python3 scripts/smoke_pty.py` verifies terminal launch/quit and
`python3 scripts/smoke_server.py` verifies detached lifecycle and crash recovery with an
isolated fake server. Neither smoke script mutates existing sessions.

## Commits and pull requests

Each commit has exactly one line, no body and no trailers:

```text
feat(ui): add team picker
fix: preserve inherited permissions
ref: simplify dashboard rendering
chore: update contributor guide
```

Allowed types: `feat`, `fix`, `ref`, `chore`. Documentation and tests use `chore`;
formatting, lint fixes and refactors use `ref`. Use a lowercase imperative subject
(proper nouns and acronyms may retain case), with no trailing period.
Lefthook validates commits and checks staged Go changes.

Describe behavior changes and verification in your pull request. Run `make check` and
`make test-race`. Include a text screenshot for UI changes. Keep unrelated changes separate.

## Releases

Update `CHANGELOG.md`, run all checks, and create a reviewed version tag when ready.
GoReleaser v2 builds macOS/Linux amd64/arm64 archives and checksums. Validate locally with
`goreleaser check` and `goreleaser release --snapshot --clean --skip=publish`.
Local builds do not publish. A maintainer can explicitly run `goreleaser release --clean`
with a GitHub token after pushing an approved tag. This creates the GitHub release and
updates the formula in `qivs/homebrew-tap`; the token needs access to that tap. No package
registry publishing or automatic release workflow is configured. Verify that the tap
exists and its permissions are correct before the first release.
