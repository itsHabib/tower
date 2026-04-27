# Testing Tower

Tower has three tiers of tests, separated by the resources they need.

## Tiers

### 1. Unit (default)

Pure-Go tests that don't shell out to git, don't touch the filesystem
beyond `t.TempDir()` for an SQLite store, and don't open subprocesses.
Run on every commit; should finish in well under a second.

```bash
go test ./...
```

What's covered: store SQL, parsing/format helpers, sort modes,
priority logic, workflow orchestration with fake `observe.Git` and
fake `observe.GH`, MCP handler unit tests with the same fakes.

### 2. Integration (build tag: `integration`)

Tests that shell out to the real `git` binary, build the real
`tower.exe`, drive the live `tui.Model` through synthetic key events,
or speak JSON-RPC to a spawned `tower mcp serve`. Each test is
hermetic — it builds its own throwaway git repo under `t.TempDir()`
and uses an isolated `APPDATA` so it can't see or corrupt your real
tower state.

```bash
go test -tags=integration ./...
```

What's covered:
- `internal/tui/tui_e2e_test.go` — drives the bubbletea `Model`
  directly via `Update(tea.KeyMsg{...})`. Bypasses the program loop, so
  it's fast but doesn't exercise the renderer. Use it for action-flow
  tests (a / r / d/y).
- `internal/tui/tui_teatest_test.go` — drives the **full bubbletea
  program loop** via [`teatest`][teatest]. Covers what the user
  actually sees: rendered output, view transitions, drill-in flow.
  Three patterns demonstrated — substring waits, FinalModel
  inspection, and golden-file snapshots of the rendered view.
- `cmd/tower/e2e_test.go` — runs the real `tower.exe` binary as a
  subprocess against a fresh repo: full add → ls → rm → re-add cycle,
  unmerged-branch warning path, MCP tool listing, MCP register/list.

[teatest]: https://pkg.go.dev/github.com/charmbracelet/x/exp/teatest

### Updating golden files

Snapshot tests under `internal/tui/testdata/` use `teatest.RequireEqualOutput`,
which compares against `<TestName>.golden`. After an intentional view
change, regenerate with:

```bash
go test -tags=integration -run TestTeatest_View_Snapshot ./internal/tui/... -args -update
```

Volatile fragments (temp paths, future timestamps) get scrubbed before
comparison — see `scrubVolatile` in `tui_teatest_test.go`. Add to it
if you introduce a new volatile bit.

## Manual testing in an isolated environment

The setup script builds tower, then runs `scripts/seed` to spin up
~6 fake repos with ~23 worktrees in mixed state (clean / dirty /
ahead-of-main, varied branch prefixes). State lands in a sandbox
`APPDATA` so it can't touch your real tower state.

```powershell
# PowerShell
powershell -File scripts\setup-test-env.ps1
```

```bash
# bash / Git Bash / WSL
bash scripts/setup-test-env.sh
```

Output ends with the exact `APPDATA=... tower.exe` invocation you
should run. Re-running the script wipes the previous sandbox.

The fixture spec lives in `internal/playground/playground.go`
(`Default`). Edit that variable to add repos / worktrees / states; both
the sandbox script and any test that imports `internal/playground`
pick up the change.

## Debug logging

Set `TOWER_DEBUG=1` before launching the TUI to enable trace logging
to `%TEMP%/tower-debug.log`. The log records every key dispatch and
every removeCmd / repoAddedMsg / loadedMsg flow. Useful when something
"doesn't seem to do anything" — the log shows whether the code path
even fired.

```bash
TOWER_DEBUG=1 ./tower.exe   # bash / Git Bash
$env:TOWER_DEBUG=1; .\tower.exe   # PowerShell
```

## Task runner (cross-platform)

Targets are in `Taskfile.yml`, run via [Task](https://taskfile.dev/):

```bash
go install github.com/go-task/task/v3/cmd/task@latest
task --list
task test          # unit tests
task test:int      # unit + integration
task build         # build tower.exe
task tui:sandbox   # build + launch TUI in isolated sandbox
```

Task is a cross-platform make replacement; it works on Windows without
needing make, bash, or WSL. The `Taskfile.yml` also documents what
each target does.

## Adding tests

- A new piece of pure logic: drop a `_test.go` next to the file with
  fakes for any external dependencies. No build tag.
- A flow that touches git or the live `tui.Model`: add to one of the
  `_e2e_test.go` files with `//go:build integration` at the top, and
  use `t.TempDir()` plus an env-isolated `APPDATA`.
- A flow that needs a real external resource: same as integration but
  also `t.Skip(...)` unless the relevant env var is set, and document
  the env var in the table above.

The split exists so `go test ./...` stays a fast feedback loop on
every save while the heavier suites can run on demand or in CI.
