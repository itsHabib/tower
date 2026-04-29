# tower

[![ci](https://github.com/itsHabib/tower/actions/workflows/ci.yml/badge.svg)](https://github.com/itsHabib/tower/actions/workflows/ci.yml)

A control tower for parallel agentic work. See, at a glance, every
worktree and the pull request it turns into — all from one place.

Tower is a TUI, a CLI, and an MCP server. Pick whichever surface fits
the moment.

> Status: early development. Tested on Windows; CLI works on every
> platform.

---

## What it's for

If you live in a single feature branch at a time, you don't need this.
Tower exists for the workflow where you're juggling four or five
in-flight changes at once, often with an AI agent driving each, and
you want to know — at a glance — which ones are dirty, which are
waiting on review, which broke CI, and which need your attention.

Mental model: each row is a worktree. Each worktree maps 1:1 to a
branch and (eventually) a PR. Tower keeps the local git state, the
GitHub PR/review/CI state, and the worktree's on-disk path all in
one place, so the answer to "what should I look at next?" is one
keystroke away.

---

## Install

```bash
go install github.com/itsHabib/tower/cmd/tower@latest
```

Or build from source:

```bash
git clone https://github.com/itsHabib/tower
cd tower
go build -o tower.exe ./cmd/tower
# (or use the Taskfile)
task build
```

You'll need `git` on PATH. For the GitHub-aware features you'll also
need `gh` authenticated.

---

## Quick start

```bash
# from inside a git repo you want tower to track:
tower repo add

# create a worktree for a new feature
tower add login-redesign
# tower creates the branch tower/login-redesign and the worktree at
# .worktrees/login-redesign

# open the board
tower
```

The TUI shows every worktree across every registered repo, sorted by
"which one needs your attention next" by default.

---

## The TUI at a glance

Two views, toggled with `g`:

**Grouped (default)** — one row per repo, summarising what's going on
across all of its worktrees. Press `enter` on a repo to drill into
that repo's worktrees.

```
tower
[?] help  [q] quit  [s] sync  [g] grouped  [/] filter  · auto-refresh 60s
[enter] drill  [a] worktree  [r] repo

  REPO                 WORKTREES DIRTY   OPEN PRS FAILING CI LAST ACTIVITY
> roxiq                5         2       3        1          5m
  tower                3         0       1        0          1h
  cortex               2         1       0        0          2d
```

**Flat** — one row per worktree. This is where you act on individual
branches: see their full state, remove them, bulk-delete a batch, open
their PR.

```
tower
[?] help  [q] quit  [s] sync  [g] flat  [/] filter  · auto-refresh 60s
[enter] details  [a] worktree  [r] repo  [d] remove  [o] open PR  [space] select  [D] delete selected

      REPO          BRANCH                DIRTY  A/B  PR             CI                    REVIEWS               LAST
>     roxiq         tower/login-redesign  yes    2/0  #142 open      ✓ 5/5                 ✓ alice               5m · refactor: split form
  ✓   roxiq         tower/auth-cleanup    -      0/1  #138 merged    ✓ 5/5                 ✓ bob                 1d · merge main
      roxiq         main                  -      0/0  -              -                     -                     just now · initial
```

The two header rows split universal controls from mode-specific
actions, so what you see is exactly what works *right now*. The `✓`
marker on the second row is a multi-select hit (toggled with
`space`); the chip in the footer becomes `N selected — D to delete`
once anything is marked.

Pressing `enter` on a flat row opens a **detail panel** with the
worktree's full state — path, ahead/behind, PR number/title/url, every
CI check, every reviewer's latest disposition. From the panel, `o`
opens the PR in your browser; `esc`/`q`/`enter` closes it.

Columns (flat):
- **DIRTY** — uncommitted changes in the worktree
- **A/B** — commits **a**head / **b**ehind the branch's upstream
- **PR** — most recent known PR state for the branch
- **CI** — pass/fail summary across all checks
- **REVIEWS** — latest disposition per reviewer
- **LAST** — age + subject of HEAD

### Keys

| Key       | Does                                                            |
|-----------|-----------------------------------------------------------------|
| `j` / `k` | move cursor                                                     |
| `enter`   | grouped: drill into the cursor repo. flat: open detail panel for the cursor row |
| `/`       | filter (substring on branch/repo/title); `esc` clears           |
| `g`       | toggle grouped-by-repo / flat                                   |
| `a`       | add a worktree to cursor row's repo (or the only repo if empty) |
| `r`       | register a new repo (path; empty = cwd)                         |
| `d`       | flat only — remove cursor worktree (deletes branch only if merged) |
| `space`   | flat only — toggle selection on cursor row (advances cursor)    |
| `D`       | flat only — remove every selected worktree                      |
| `o`       | flat only — open cursor row's PR in the browser                 |
| `s`       | sync from git + GitHub now                                      |
| `?`       | help screen                                                     |
| `q`       | quit                                                            |

### Removing worktrees safely

`d` removes the worktree and tries to delete the branch with
`git branch -d` (the safe variant). If the branch has unmerged
commits, the worktree is gone but the branch is preserved and tower
shows you the exact `git branch -D` command to discard the work if
you really want to.

The main worktree of a repo is refused outright — you can't
accidentally tear down the primary checkout.

---

## CLI

Same operations, scriptable. Useful in CI, hooks, or just from
muscle memory.

```bash
# repos
tower repo add [path]            # register; defaults to cwd
tower repo ls                    # list registered
tower repo rm <name>             # unregister
tower repo prune [--dry-run]     # drop repos whose path is gone

# worktrees (the --repo flag wins; cwd-inference is the fallback)
tower add <name> [--repo r]      # new tower-style worktree
tower rm <name> [--repo r]       # remove worktree (and its branch if merged)
tower ls                         # show the board as a table

# state
tower sync                       # reconcile + GitHub refresh
tower open <name> [--repo r]     # open the PR for a branch in browser

# editor / shell
tower shell <name> [--repo r]    # cd into the worktree (prints the path)
```

Every CLI op uses the same `workflow.Service` the TUI uses, so
behavior is identical.

---

## MCP server

Tower exposes its surface as a [Model Context Protocol](https://modelcontextprotocol.io)
server, so an agent (Claude Code, Cursor, claude.ai with a connector,
etc.) can call into it directly.

Register it in your `.mcp.json`:

```json
{
  "mcpServers": {
    "tower": {
      "command": "tower",
      "args": ["mcp", "serve"]
    }
  }
}
```

Tools the server exposes:

| Tool              | What it does                                                  |
|-------------------|---------------------------------------------------------------|
| `list_worktrees`  | Every tracked worktree with PR/CI/review state. Optional repo filter. |
| `get_worktree`    | Lookup by `{repo, branch}` or by short `{name}`.              |
| `add_worktree`    | New tower-style worktree in a registered repo.                |
| `remove_worktree` | git remove + drop from store. Refuses main worktrees.         |
| `sync`            | Full reconcile + GitHub refresh.                              |
| `reconcile`       | Local-only refresh (no GitHub calls).                         |
| `list_repos`      | Every registered repo.                                        |
| `register_repo`   | Register a repo by path.                                      |
| `unregister_repo` | Drop a repo (cascades worktrees / PR state).                  |
| `prune_repos`     | Drop repos whose path no longer exists. `dry_run` available.  |

Typical agent prompts that work well:

> "List my dirty tower worktrees and tell me which ones have failing CI."

> "Make a tower worktree called `auth-fix` in the `roxiq` repo and
> draft a PR description based on the diff against main."

---

## Concepts

### Worktrees, not clones

Tower uses `git worktree` so every branch lives as a separate working
directory under one shared `.git` (or two, in the case of bare-repo
setups). Switching contexts costs an `enter`-key, not a stash + branch
switch + dependency reinstall.

### Tower-owned branches

Branches tower creates are prefixed `tower/` so they're easy to spot
and mass-clean later. You can also bring in branches you made
yourself — tower discovers every worktree on `git worktree list`,
prefix or not.

### Worktree-as-row

Everything in the TUI is keyed on `(repo, branch)`. A PR, its
reviews, and its CI are attached to that pair. Add a new worktree →
new row. Merge a PR → row stays until the worktree is removed (or the
branch is gone), so you keep the post-merge context.

---

## Configuration

Tower stores its state in `<UserConfigDir>/tower/state.db` (a single
SQLite file). On Windows that's `%APPDATA%\tower\state.db`.

Override the location by setting `APPDATA` before launching — useful
for sandboxed testing (see `scripts/setup-test-env.sh`).

There is no config file. Defaults: branches use prefix `tower/`,
worktrees land at `<repo>/.worktrees/<name>`.

---

## Testing

See [TESTING.md](TESTING.md). Two tiers:

```bash
task test          # unit (fast, no shellouts)
task test:int      # adds integration (real git, real tower.exe, MCP server, teatest)
task tui:sandbox   # isolated TUI sandbox for manual poking
```

For a sandbox without `task` installed:

```powershell
# PowerShell
powershell -File scripts\setup-test-env.ps1
```

```bash
# bash / Git Bash
bash scripts/setup-test-env.sh
```

---

## License

MIT. See [LICENSE](LICENSE).
