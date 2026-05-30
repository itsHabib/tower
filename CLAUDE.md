# Notes for agents working on this repo

Read this before touching code. The codebase is small (~5k LoC of Go) but
opinionated.

## What tower is

A worktree observer. The board surfaces every git worktree across every
registered repo plus the GitHub state attached to each branch (PR / CI /
reviews). It is **not** an agent host — it does not spawn or manage
external processes. That scope is locked. See [the project memory note
in the user's portfolio](https://github.com/itsHabib/tower/issues) for
the pivot history; the short version: an earlier `claude-spawn` surface
was ripped out so the core stays minimal.

If a feature pulls toward orchestration, agent management, or "let tower
run X", it belongs in [orchestra](https://github.com/itsHabib/orchestra),
not here.

## Architecture

```
internal/
  domain/      # plain types: Repo, Worktree, PullRequest, Review, CICheck
  store/       # SQLite persistence (sqlite.go, schema in code)
  observe/     # thin wrappers over `git` and `gh` shellouts (Git / GH ifaces)
  refresh/     # walks repos, asks observe.* for live state, writes to store
  workflow/    # composed Service that the CLI / TUI / MCP all call into
  tui/         # bubbletea Model + view rendering (grouped + flat + detail panel)
  mcp/         # MCP server exposing the workflow as tools
  playground/  # synthetic fixture builder (used by scripts/seed AND by tests)
cmd/
  tower/       # the binary — main, subcommands, e2e_test driving the real binary
scripts/
  seed/        # CLI wrapper around playground.Seed for the manual sandbox
  setup-test-env.{sh,ps1}  # build + seed playground into ./.sandbox/
```

Dependency direction is strict: `domain → store → {observe,refresh} →
workflow → {cmd,tui,mcp}`. Don't introduce cycles or a downward import.

## Testing tiers

- **Unit** (`go test ./...`): fast, no shellouts. Covers store SQL,
  format/sort logic, MCP handlers (with fake `observe.Git` / `observe.GH`),
  workflow orchestration.
- **Integration** (`go test -tags=integration ./...`): real `git`, real
  `tower.exe`, full bubbletea program loop via
  [`teatest`](https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest).
  Each test gets its own `t.TempDir()` plus an isolated state dir
  (APPDATA + XDG_CONFIG_HOME + HOME — all three are needed for
  cross-platform isolation).
- CI runs both tiers on `ubuntu-latest`. See `.github/workflows/ci.yml`.

Adding a new test: drop a `_test.go` next to the file. If it shells out
to git or builds the binary, add `//go:build integration` at the top.

## Lint

`golangci-lint v2` strict config in `.golangci.yml`. CI fails on any
issue. Don't add `//nolint` directives without justification — refactor
instead. Past pattern: long bubbletea `Update` and `handleActionKey`
functions got split into named per-message and per-key handlers rather
than nolint'd. Same approach next time.

`gofmt`, `goimports` run as formatters. The lint job will catch
unformatted files.

## Conventions

- **Branches** tower creates are prefixed `tower/`. User-named branches
  are first-class too — reconcile picks up every worktree on
  `git worktree list`, prefix or not.
- **Worktrees** land at `<repo>/.worktrees/<name>` by default
  (`workflow.Config.WorktreeBase`).
- **State** lives at `<UserConfigDir>/tower/state.db` (one file, no
  config file). Override the location for tests by setting all three
  of `APPDATA`, `XDG_CONFIG_HOME`, `HOME` to a temp dir.
- **Errors should not be capitalized** (Go convention; staticcheck ST1005
  enforces). Wrap inner errors trimmed to the first line so multi-line
  git stderr (`hint:` lines) doesn't bleed into the TUI footer — see
  `firstLine` in `tui.go` and the `branch -d` wrap in `workflow.go`.
- **`ErrBranchKeptUnmerged`** signals "worktree gone, branch kept" — the
  user's intent (remove the worktree) succeeded; the branch ref just
  remains. Treat it as success-with-caveat in any new bulk operation,
  not as a failure. The existing `removeManyCmd` does this.

<!-- BEGIN eng-philo (managed by /eng-philo — re-run to refresh; hand-edits inside this block will be overwritten) -->
## Engineering principles

How code is written here — Dave Cheney lineage ([Practical Go](https://dave.cheney.net/practical-go)): simplicity, clarity, line-of-sight. Apply on every change; the lint below catches the slips.

1. **No `else` — line-of-sight.** Handle errors / edge cases with early returns and guard clauses; keep the happy path un-indented, flowing down the left margin. Reaching for `else` → return early instead.
2. **Shallow nesting — ≤2 levels *per scope*.** A `for` + an `if` is the ceiling in one scope. The budget is per-scope, not per-function — a closure / anon fn is its own scope, so a `for`+`if` inside a closure is fine. Deeper in one scope → extract a function.
3. **Policy vs mechanism.** Separate the decisions (policy: validation, state machines, business rules) from the plumbing (mechanism: persistence, transport, I/O). Mechanism is dumb and swappable; policy lives in a layer above it. Never let policy leak into a mechanism layer.
4. **Composition of single-responsibility layers.** Each layer / package owns ~one responsibility; the app is a *composition* of them; any piece is swappable without rippling into the others. Dependencies flow one direction.
5. **Small, sharp APIs.** Export the least callers need. Intention-revealing names. Accept the narrowest input, return concrete types. Make the zero value useful.
6. **Errors are values; simplicity over cleverness.** Handle or propagate errors explicitly — never swallow. Readable > clever > short. A little copying beats a premature abstraction or dependency.

### Go idioms + enforcement

Accept interfaces, return structs; small interfaces (1–2 methods); errors lowercase + wrapped (`%w`); early-return / line-of-sight.

*Enforce:* golangci-lint — `gocognit`, `nestif`, `cyclop`, `revive`.
<!-- END eng-philo -->

## Manual TUI sandbox

```bash
bash scripts/setup-test-env.sh        # or .ps1 on PowerShell
# launch with the printed APPDATA=... command
```

Builds 6 fake repos (alpha/beta/gamma/delta/epsilon/zeta — Greek letters
on purpose, so a script bug can't ever hit a real repo by name) with 23
worktrees in mixed clean/dirty/ahead state. Edit
`internal/playground/playground.go` (`Default`) to tweak the spread.

## Common gotchas

- Tests that build `tower.exe` and run it must override `APPDATA`,
  `XDG_CONFIG_HOME`, AND `HOME` — the first one alone only works on
  Windows. See `cmd/tower/e2e_test.go` `runCLI`.
- The grouped view's cursor tracks **repo summaries**, not worktrees.
  Anything keyed on `(repo, branch)` (delete, open-PR, multi-select)
  errors out in grouped mode with a hint to drill or switch to flat.
- The teatest snapshot at `internal/tui/testdata/` scrubs temp paths
  and the "synced Xs ago" timer via `scrubVolatile`. If you add a new
  volatile fragment to the rendered view, extend that function.
- `golangci-lint`'s `unparam` linter catches "this arg is always X" —
  the playground's `runIn(name="git", …)` got flagged this way and
  became `runGit`. Don't reintroduce dead parameters.

## When the agent is stuck

- "I see lint errors I don't understand" → run `golangci-lint run` locally
  to see them in context, not just the CI line. Most are real.
- "Tests pass locally but CI fails" → almost always state-isolation:
  the test isn't overriding all three env vars (APPDATA / XDG_CONFIG_HOME /
  HOME), or it's writing under `t.TempDir()` but a sibling test sees it.
- "I want to add a feature that makes tower run something" → stop. Pivot
  was deliberate. Check with the user before extending scope past
  observation.
