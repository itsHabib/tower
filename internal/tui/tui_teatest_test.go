//go:build integration

// teatest-driven integration tests for the TUI. These run the full
// bubbletea program loop (not the bypass that tui_e2e_test.go uses)
// so timing, command dispatch, and view rendering all match what a
// real user sees. Cribs from `scripts/seed` for fixture shape but
// uses a smaller subset to keep each test under a couple of seconds.
//
// Three patterns are demonstrated; copy whichever fits the case:
//
//   1. WaitFor + substring — assert a string appears in the rendered
//      output. Robust against minor view changes.
//   2. Send keys — drive the model through a flow and inspect either
//      output or the FinalModel.
//   3. RequireEqualOutput — full golden-file snapshot. Update with
//      `go test -tags=integration -update ./internal/tui/...`. Brittle
//      to view tweaks but high-signal when you want to lock layout.
//
// Run with `go test -tags=integration ./internal/tui/...`.
package tui

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/playground"
	"github.com/itsHabib/tower/internal/refresh"
	"github.com/itsHabib/tower/internal/store"
	"github.com/itsHabib/tower/internal/workflow"
)

// teatestFixture wraps an e2e fixture with the inputs teatest needs.
// Same shape as e2eFixture but with a no-op GH client so background
// sync calls stay local — otherwise tower would shell out to `gh` and
// either hang or write error rows the snapshots have to account for.
type teatestFixture struct {
	store store.Store
	wf    *workflow.Service
}

// newTeatestFixture builds a fresh sandbox under t.TempDir(), seeds it
// with `fix`, and returns the wired-up workflow + store.
func newTeatestFixture(t *testing.T, fix []playground.Repo) *teatestFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}

	dbPath := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()
	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	gitFactory := func(p string) observe.Git { return observe.NewGit(p) }
	ghFactory := func(p string) observe.GH { return noopGH{} }
	ref := refresh.New(s, gitFactory, ghFactory)
	wf := workflow.New(workflow.Config{}, s, gitFactory, ref)

	repoRoot := filepath.Join(t.TempDir(), "repos")
	if _, err := playground.Seed(ctx, wf, repoRoot, fix, nil); err != nil {
		t.Fatalf("seed playground: %v", err)
	}
	return &teatestFixture{store: s, wf: wf}
}

// noopGH stands in for the gh-shelling client. Returning empty results
// (not errors) keeps the sync sweep silent — no PR/CI/review rows show
// up, no error banner either. If you want to assert on those columns,
// build a richer fake with the data you need.
type noopGH struct{}

func (noopGH) PullRequestForBranch(ctx context.Context, branch string) (*domain.PullRequest, error) {
	return nil, nil
}
func (noopGH) Reviews(ctx context.Context, prNumber int) ([]domain.Review, error) { return nil, nil }
func (noopGH) Checks(ctx context.Context, prNumber int) ([]domain.CICheck, error) { return nil, nil }

// miniFixture is a 3-repo / 4-worktree spread that exercises both
// branch-prefix flavours and the dirty / ahead states without taking
// the multiple seconds the full playground.Default needs to seed.
var miniFixture = []playground.Repo{
	{Name: "alpha", Worktrees: []playground.Worktree{
		{Name: "feat-a", BranchPrefix: "tower/", ExtraCommits: 2},
		{Name: "wip-a", BranchPrefix: "tower/", Dirty: true},
	}},
	{Name: "beta", Worktrees: []playground.Worktree{
		{Name: "feat-b", BranchPrefix: "feat/", ExtraCommits: 1},
	}},
	{Name: "gamma", Worktrees: []playground.Worktree{
		{Name: "wip-g", BranchPrefix: "wip/", Dirty: true, ExtraCommits: 3},
	}},
}

// newTestModel boots the TUI with the mini fixture, sized to a
// generous viewport, and returns the live teatest TestModel ready for
// keystrokes.
func newTestModel(t *testing.T) *teatest.TestModel {
	t.Helper()
	return newTestModelWith(t, miniFixture)
}

// newTestModelWith is the variant for tests that need a non-default
// fixture (e.g. one where every selectable row is cleanly deletable).
func newTestModelWith(t *testing.T, fix []playground.Repo) *teatest.TestModel {
	t.Helper()
	f := newTeatestFixture(t, fix)
	m := newModel(context.Background(), f.wf, f.store)
	return teatest.NewTestModel(t, m, teatest.WithInitialTermSize(140, 40))
}

// waitForOutput blocks until the rendered output contains all of `subs`,
// or fails the test. Wraps teatest.WaitFor with a sane default timeout
// so tests don't hang if a view never lands.
func waitForOutput(t *testing.T, tm *teatest.TestModel, subs ...string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(out []byte) bool {
		for _, s := range subs {
			if !bytes.Contains(out, []byte(s)) {
				return false
			}
		}
		return true
	}, teatest.WithDuration(3*time.Second), teatest.WithCheckInterval(50*time.Millisecond))
}

// TestTeatest_GroupedView_RendersAllRepos boots the TUI and waits for
// every repo from miniFixture to appear as a row in the grouped view.
func TestTeatest_GroupedView_RendersAllRepos(t *testing.T) {
	tm := newTestModel(t)
	waitForOutput(t, tm, "REPO", "WORKTREES", "alpha", "beta", "gamma")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestTeatest_DrillIntoRepo presses enter on the cursor row in grouped
// view, asserts the model switches to flat with the repo's name set as
// the filter so only that repo's worktrees show.
func TestTeatest_DrillIntoRepo(t *testing.T) {
	tm := newTestModel(t)
	waitForOutput(t, tm, "alpha", "beta", "gamma")

	// Cursor lands on the highest-priority repo at row 0. The mini
	// fixture's priority story doesn't matter — we just need to be on
	// some repo row, then read its name back from the FinalModel after
	// drilling in.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	// After enter: mode should flip to flat and a filter should be set.
	// Wait for the filter line to render so we know the model has
	// applied the drill.
	waitForOutput(t, tm, "filter:")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))

	final := tm.FinalModel(t).(*Model)
	if final.mode != ViewFlat {
		t.Fatalf("after enter on grouped row: mode=%v want ViewFlat", final.mode)
	}
	if final.filter == "" {
		t.Fatal("after drill: filter should be set to the repo's name")
	}
	visible := final.visibleRows()
	if len(visible) == 0 {
		t.Fatalf("after drill (filter=%q): no visible rows", final.filter)
	}
	for _, r := range visible {
		if !strings.Contains(strings.ToLower(r.wt.Repo), strings.ToLower(final.filter)) {
			t.Errorf("visible row %s/%s leaked across filter %q", r.wt.Repo, r.wt.Branch, final.filter)
		}
	}
}

// TestTeatest_DetailPanel drills into a repo (grouped → flat) and
// opens the detail panel on the first worktree row. Asserts that path,
// ahead/behind, and the per-section labels render.
func TestTeatest_DetailPanel(t *testing.T) {
	tm := newTestModel(t)
	waitForOutput(t, tm, "alpha")

	// Grouped: enter drills into the first repo (highest priority is
	// gamma per miniFixture, but any row works for this assertion).
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "filter:")

	// Flat: enter on the cursor row opens the detail panel.
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitForOutput(t, tm, "path", "ahead/behind", "PR", "CI", "REVIEWS")

	// esc closes the panel back to flat view.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	waitForOutput(t, tm, "BRANCH")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// deletableFixture has worktrees that are cleanly removable: no extra
// commits beyond main (so `git branch -d` succeeds without the
// unmerged-commits gate) and a mix of clean / dirty so the
// dirty-warning path through removeManyCmd is also exercised.
var deletableFixture = []playground.Repo{
	{Name: "alpha", Worktrees: []playground.Worktree{
		{Name: "clean", BranchPrefix: "tower/"},
		{Name: "dirty", BranchPrefix: "tower/", Dirty: true},
	}},
}

// TestTeatest_MultiSelectAndDelete switches to flat view, marks two
// rows with space, hits D, confirms with y, and asserts the success
// banner + cleared selection.
func TestTeatest_MultiSelectAndDelete(t *testing.T) {
	tm := newTestModelWith(t, deletableFixture)
	waitForOutput(t, tm, "alpha")

	// Switch to flat — multi-select is flat-only.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	waitForOutput(t, tm, "BRANCH")

	// deletableFixture has 2 worktrees + 1 main = 3 rows. Priority
	// sort puts dirty first, so cursor is on tower/dirty at row 0.
	// Space selects + advances → row 1 (tower/clean). Space again
	// selects + advances → row 2 (main). We want rows 0 and 1, not 2,
	// so use space then j (move) then space.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	waitForOutput(t, tm, "2 selected — D to delete")

	// D opens the multi-confirm prompt; "1 DIRTY" warning appears
	// because tower/dirty was in the selection.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	waitForOutput(t, tm, "remove 2 selected worktrees", "1 DIRTY")

	// y dispatches the bulk delete; loadCmd reloads, info banner shows.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	waitForOutput(t, tm, "removed 2 worktrees")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))

	final := tm.FinalModel(t).(*Model)
	if len(final.selected) != 0 {
		t.Fatalf("after success: %d rows still selected, want 0", len(final.selected))
	}
}

// TestTeatest_BulkDelete_BranchKept_TreatedAsSuccess covers the case
// where a selected worktree had unmerged commits — the worktree should
// still be removed, just the branch ref kept. The summary should
// reflect that as success-with-caveat ("X branch refs kept"), not as a
// failure.
func TestTeatest_BulkDelete_BranchKept_TreatedAsSuccess(t *testing.T) {
	fix := []playground.Repo{
		{Name: "alpha", Worktrees: []playground.Worktree{
			// Two worktrees with extra commits — `git branch -d` will
			// refuse and return ErrBranchKeptUnmerged. The worktrees
			// themselves still come down.
			{Name: "ahead-one", BranchPrefix: "tower/", ExtraCommits: 2},
			{Name: "ahead-two", BranchPrefix: "tower/", ExtraCommits: 3},
		}},
	}
	tm := newTestModelWith(t, fix)
	waitForOutput(t, tm, "alpha")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	waitForOutput(t, tm, "BRANCH")

	// Both ahead rows have priority "none" (no dirty / no PR), so
	// they sort by activity. Either way they're the first two rows
	// (main is third). Select both.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	waitForOutput(t, tm, "2 selected — D to delete")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	waitForOutput(t, tm, "remove 2 selected worktrees")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	// Worktrees are gone, both branches kept.
	waitForOutput(t, tm, "removed 2 worktrees", "2 unmerged branch refs kept")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))

	final := tm.FinalModel(t).(*Model)
	if len(final.selected) != 0 {
		t.Fatalf("after branch-kept success: %d still selected, want 0 (worktrees were removed)", len(final.selected))
	}
}

// TestTeatest_HelpScreen presses ? and asserts the help text appears.
func TestTeatest_HelpScreen(t *testing.T) {
	tm := newTestModel(t)
	waitForOutput(t, tm, "alpha")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	waitForOutput(t, tm, "TOWER", "NAVIGATION", "ACTIONS", "Press ? or esc to dismiss")

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

// TestTeatest_View_Snapshot demonstrates golden-file diffing on the
// rendered view. Snapshots the final model's View() output (not the
// raw program output stream — that gets drained by waitForOutput
// above, leaving only termination escapes for FinalOutput to read).
//
// Update the golden with:
//
//	go test -tags=integration -run TestTeatest_View_Snapshot ./internal/tui/... -args -update
//
// Use snapshot tests for layout-locking; use substring waits for
// behaviour. Any column-width or styling change rewrites the golden.
func TestTeatest_View_Snapshot(t *testing.T) {
	tm := newTestModel(t)
	// Wait for content AND for the background sync to complete. Without
	// the sync wait the snapshot is racey: on a fast Linux runner sync
	// finishes before the snapshot, on Windows it usually doesn't, and
	// the renderer emits different header / footer in each case
	// ("◯ syncing…" before vs. "synced Xs ago" after).
	waitForOutput(t, tm, "alpha", "beta", "gamma")
	waitForOutput(t, tm, "synced")

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))

	final := tm.FinalModel(t).(*Model)
	teatest.RequireEqualOutput(t, []byte(scrubVolatile(final.View())))
}

// scrubVolatile replaces per-run-changing fragments of the rendered
// view with stable placeholders so golden diffs only reflect real
// layout changes:
//   - the cursor-row path footer (embeds t.TempDir()),
//   - "synced Xs ago" (counts up between sync completion and snapshot).
var (
	tempPathRE = regexp.MustCompile(`(?i)[a-z]:\\[^\r\n]+temp\\[^\r\n]+`)
	unixTmpRE  = regexp.MustCompile(`/(?:tmp|var/folders/[^\s]+)/[^\r\n]+`)
	syncTimeRE = regexp.MustCompile(`synced \S+ ago`)
)

func scrubVolatile(s string) string {
	s = tempPathRE.ReplaceAllString(s, "<TMP_PATH>")
	s = unixTmpRE.ReplaceAllString(s, "<TMP_PATH>")
	s = syncTimeRE.ReplaceAllString(s, "synced <AGE> ago")
	return s
}
