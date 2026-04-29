---
name: ship-feature
description: Take a design doc through to a PR with reviews requested. Implements the doc, opens the PR, iterates locally on CI until green, then waits for reviews and addresses every actionable comment — surfacing anything ambiguous or sweeping back to the user.
argument-hint: "[design-doc-path] — defaults to the most recently committed design doc on main"
user_invocable: true
---

# /ship-feature -- Design Doc → PR → Reviews

Drive a single piece of work end-to-end: pick up a design doc, implement it on a feature branch, open a PR with reviews requested from `@claude` / `@codex` / Copilot, baby-sit CI in the active session, then wait for the three reviews to land and address every actionable comment — surfacing anything ambiguous or sweeping back to you via `AskUserQuestion`.

## Arguments

Parse `<user_argument>` as: `[design-doc-path]`

- `design-doc-path` (optional): path to a markdown design doc. If omitted, auto-discover the most recent design doc committed on `main`.

Store as `$DOC`.

## Steps

### 1. Resolve the design doc

If `$DOC` was given as an argument, verify the file exists. If it doesn't, abort with a clear message — do nothing else.

If `$DOC` was NOT given, ASK the user — do not auto-infer. Use `AskUserQuestion` with the up-to-3 most recent design docs from `main` as options for one-click selection:

```bash
git log main --diff-filter=A --name-only --pretty=format: -- 'docs/features/*.md' 'docs/prompts/*.md' 'docs/design/*.md' 'docs/rfcs/*.md' \
  | awk 'NF' | head -3
```

Build the options list:
- Each found doc becomes an option (label = filename, description = "<path> — last touched <date>" if cheap, otherwise just the path).
- If 0–1 docs were found, add an explicit "I'll provide a different path" option so the question has ≥2 options.
- The auto-added "Other" option lets the user type a path freely.

Branch on the user's answer:
- Picked a listed doc → set `$DOC` to that path.
- Picked "Other" / "I'll provide a different path" → take their typed answer as the path. Verify it exists; if not, abort with a clear message.

Read the doc into `$DOC_CONTENT` and derive `$SLUG` from the filename (strip extension, replace any non-alphanumeric with `-`, e.g. `09-foo.md` → `09-foo`).

If a remote branch `feature/$SLUG` already exists (`git ls-remote --heads origin "feature/$SLUG"`), append `-2`, `-3`, etc. until you find a free name. Final value goes in `$BRANCH`.

### 2. Implement the design doc

Implement the design doc on a new branch named `$BRANCH` cut from current `main`. How the implementation gets done is up to the caller — directly, via a subagent, in a worktree, etc. The skill is agnostic about the mechanism. Whatever path is chosen, after this step `$BRANCH` must exist with the implementation committed and the working copy free of staged changes that don't belong on the PR.

Implementation guidelines (the implementer must follow these):

```
You are implementing a design doc end-to-end.

Design doc path: <$DOC>
Branch to land work on: <$BRANCH>

--- DESIGN DOC CONTENT ---
<$DOC_CONTENT>
--- END DESIGN DOC ---

Repo conventions: read .claude/CLAUDE.md (or CLAUDE.md at repo root) if it exists and follow whatever it states — atomic file writes, package layout, testing patterns, lint rules, anything else specified. Do NOT force-push to a PR branch — fix issues with new commits.

Your job:
1. Implement the design doc in full. Read the existing code; reuse existing utilities where they fit.
2. Build / test / lint per the repo's conventions before committing. Typical commands by language:
   - Go: `make vet && make test`, or `go vet ./... && go test ./...` if no Makefile
   - Node: `pnpm test` / `npm test` / `yarn test` (check package.json scripts)
   - Rust: `cargo build && cargo test`
   - Python: `pytest` (check pyproject.toml / setup.cfg for the canonical command)
   If the repo has a CI workflow file (`.github/workflows/*.yml`), the commands it runs are authoritative.
3. Commit in logical chunks with descriptive messages.
4. Do NOT push. Do NOT open a PR. The calling session will do that.
```

If the implementation fails or produces no commits, abort: do NOT push, do NOT open a PR. Print a clear failure summary so the user can inspect the branch (or the working copy) manually.

### 3. Push and open the PR

Push the branch and open the PR:

```bash
git push -u origin "$BRANCH"
```

Build a PR title from the doc's H1 (or filename if there's no H1). Build the body from the doc's first paragraph plus a test plan. Then:

```bash
PR_URL=$(gh pr create \
  --base main --head "$BRANCH" \
  --title "<derived title>" \
  --body "$(cat <<'EOF'
## Summary
<2-3 bullet points distilled from the design doc>

## Design doc
<relative link to $DOC on main>

## Test plan
- [ ] Build / test / lint per repo conventions pass
- [ ] <doc-specific verification steps>

Generated with /ship-feature
EOF
)")
PR_NUMBER=$(echo "$PR_URL" | grep -o '[0-9]*$')
```

Capture both `$PR_URL` and `$PR_NUMBER`.

### 4. Fan out review requests

Three commands, in this exact shape — `@claude review` and `@codex review` as separate comments, Copilot as a requested reviewer. Both bots take the literal trigger word `review` after the mention; bare `@claude` / `@codex` is unreliable. Do NOT comment `@copilot` (it pushes commits).

```bash
gh pr comment "$PR_NUMBER" --body "@claude review"
gh pr comment "$PR_NUMBER" --body "@codex review"
gh pr edit "$PR_NUMBER" --add-reviewer Copilot
```

If the Copilot reviewer add fails (handle differs per repo/org), retry with `copilot-pull-request-reviewer`. If both fail, print a one-liner asking the user to add Copilot manually and continue — don't block on this.

### 5. Iterate locally on CI until green

Invoke `/loop` in dynamic mode (no interval — model self-paces) with the body prompt below. The loop runs in the active session so the user can see each iteration and Ctrl-C if needed.

Loop body prompt:

```
Polling CI for PR #<$PR_NUMBER>.

1. Run: gh pr checks <$PR_NUMBER> --json name,state,conclusion
2. Classify each check:
   - SUCCESS → counted as passing
   - IN_PROGRESS / QUEUED / PENDING → still running
   - FAILURE / CANCELLED / TIMED_OUT / ACTION_REQUIRED → failed

3. Decide:
   - All checks SUCCESS → DONE. Print "CI green ✓" and do NOT call ScheduleWakeup (this exits the loop).
   - Any failure → fetch logs with `gh run view <run-id> --log-failed`, fix the underlying issue, commit with a descriptive message, push. Then ScheduleWakeup(~270s) to re-check.
   - Otherwise (still running) → ScheduleWakeup(~270s) to re-check.

Never use destructive git operations. Never force-push. Fix the actual bug — do not skip tests or hooks.
```

When the loop exits naturally (CI green), proceed to step 6.

### 6. Wait for reviews and address comments

Invoke `/loop` in dynamic mode (no interval — model self-paces) with the body prompt below. The loop runs in the active session so review comments can be triaged with `AskUserQuestion` when needed.

Loop body prompt:

```
Wait for the three reviewers (@claude, @codex, Copilot) on PR #<$PR_NUMBER> at <$PR_URL>, and address every actionable comment they leave.

Each iteration:

1. Fetch PR state:
     gh pr view <$PR_NUMBER> --json reviews,comments,reviewRequests,state
     gh api repos/{owner}/{repo}/pulls/<$PR_NUMBER>/comments
   (the second call returns inline review comments with stable IDs)

2. Determine which reviewers have responded:
   - @claude → a comment exists whose author login is "claude" or matches the claude bot app
   - @codex → a comment exists whose author login is "codex" or matches the codex bot app
   - Copilot → an entry exists in `reviews` whose author is Copilot (and Copilot is no longer in `reviewRequests`)

3. Build the list of unaddressed actionable comments. To track state across iterations, every fix-commit you push MUST include a trailer line `Addresses: <comment-id>` (one per comment). Comments with no matching trailer in `git log` and no prior `AskUserQuestion` "skip" decision are still pending.

4. For each unaddressed actionable comment, classify and act:
   - **Clear-actionable** (concrete, scoped, unambiguous — e.g. "rename foo to bar", "return early when nil"): make the fix, run the project's build/test/lint, commit (with the `Addresses: <id>` trailer), push.
   - **Ambiguous** (multiple interpretations, scope unclear, or a question rather than an ask): call `AskUserQuestion` with concrete options, e.g. "Apply the change as suggested / Reply on PR explaining why we won't / Skip". Act on the answer; if the answer is reply, post via `gh pr comment`.
   - **Sweeping** (large refactor, rearchitecture, multi-file rewrite, or anything touching >~3 files): ALWAYS `AskUserQuestion` to confirm scope before touching code. Do not silently make large changes.
   - **Non-actionable** (LGTM, praise, low-priority nits the user has previously skipped): mark resolved without action.

5. CI gate: if you pushed in this iteration, do NOT declare any of the freshly-pushed comments resolved until CI is green again. Re-poll `gh pr checks <$PR_NUMBER>` next iteration; on failure fix and re-push.

6. Decide loop control:
   - All three reviewers responded AND zero unaddressed actionable comments AND CI green → DONE. Print "Reviews addressed ✓" and do NOT call ScheduleWakeup (this exits the loop).
   - Pushed fixes this iteration → ScheduleWakeup(~270s) so CI has time to run before the next check.
   - Otherwise (waiting on a reviewer or on user input) → ScheduleWakeup(~600s); reviews land slowly so wider polling is fine.

Never force-push. Never push code that hasn't passed local build/test. If a fix is too unclear to attempt, ask via `AskUserQuestion` rather than guessing.
```

When the loop exits naturally (all reviews in, all comments addressed, CI green), proceed to step 7.

### 7. Final summary and exit

Print:

```
--- /ship-feature complete ---
Doc:      <$DOC>
Branch:   <$BRANCH>
PR:       <$PR_URL>
CI:       green ✓
Reviews:  addressed ✓ — PR is ready for you to merge
```

## Important

- This skill is a **single bundled approval**: once the user invokes `/ship-feature`, run all seven steps without re-prompting between them. The only allowed prompts are the doc-selection question in step 1 (when no path arg is given) and the per-comment `AskUserQuestion` calls in step 6 for ambiguous or sweeping review feedback.
- Never force-push. Never skip hooks. If a CI fix would require either, stop the loop and surface it to the user instead.
- If implementation in step 2 fails, abort BEFORE push. Print enough context (branch name, last commit if any) for the user to inspect manually.
- Copilot's reviewer handle differs by org. Don't block the workflow if `--add-reviewer` fails; print a one-liner and continue.
- Step 6 runs in the active session so it can use `AskUserQuestion`. If the user Ctrl-Cs the loop and walks away, no notification is auto-scheduled — they can re-invoke `/ship-feature` later or address comments by hand.
- If CI never goes green in step 5, the user can Ctrl-C. Step 6 is never reached and the skill exits cleanly with no scheduled tasks to clean up.
