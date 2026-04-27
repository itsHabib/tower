//go:build integration

// Integration tests that drive the live tui.Model against a real
// throwaway git repo. They exercise every keystroke flow that the user
// reaches in the TUI: register repo (r), add worktree (a), remove
// worktree (d/y).
//
// These touch the filesystem (mkdir, git init, git worktree add/remove)
// and shell out to the real git binary, so they are tagged
// `integration` and excluded from default `go test`. Run them with
// `go test -tags=integration ./...`.
package tui

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsHabib/tower/internal/observe"
	"github.com/itsHabib/tower/internal/refresh"
	"github.com/itsHabib/tower/internal/store"
	"github.com/itsHabib/tower/internal/workflow"
)

// e2eFixture is a hermetic test environment: a fresh git repo + a fresh
// SQLite store + a wired-up workflow, all under t.TempDir().
type e2eFixture struct {
	repoPath string
	store    store.Store
	wf       *workflow.Service
}

func newE2EFixture(t *testing.T) *e2eFixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}

	repoPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-qm", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	dbPath := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()
	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	gitFactory := func(p string) observe.Git { return observe.NewGit(p) }
	ghFactory := func(p string) observe.GH { return observe.NewGH(p) }
	ref := refresh.New(s, gitFactory, ghFactory)
	wf := workflow.New(workflow.Config{}, s, gitFactory, ref)

	if _, err := wf.AddRepo(ctx, repoPath, "repo"); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := wf.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	return &e2eFixture{repoPath: repoPath, store: s, wf: wf}
}

// boot returns a Model with the initial load and a 200x50 viewport so
// the cursor / row layout is ready for input.
func (f *e2eFixture) boot(t *testing.T) *Model {
	t.Helper()
	ctx := context.Background()
	m := newModel(ctx, f.wf, f.store)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = mAny.(*Model)
	mAny, _ = m.Update(loadCmd(ctx, f.wf, f.store)())
	return mAny.(*Model)
}

// typeRunes feeds each rune as a separate KeyMsg so KeyRunes handlers
// see one rune at a time, matching how bubbletea actually delivers them
// from a real terminal.
func typeRunes(t *testing.T, m *Model, s string) *Model {
	t.Helper()
	for _, ch := range s {
		mAny, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		m = mAny.(*Model)
	}
	return m
}

func keyEnter(t *testing.T, m *Model) (*Model, tea.Cmd) {
	t.Helper()
	mAny, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return mAny.(*Model), cmd
}

func keyRune(t *testing.T, m *Model, r rune) (*Model, tea.Cmd) {
	t.Helper()
	mAny, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return mAny.(*Model), cmd
}

// drainCmd runs the dispatched tea.Cmd, feeds the resulting message
// back into Update, and runs whatever follow-up command Update queued.
// Two layers of draining covers the typical mutate → loadCmd flow.
func drainCmd(t *testing.T, m *Model, cmd tea.Cmd) *Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := cmd()
	mAny, next := m.Update(msg)
	m = mAny.(*Model)
	if next != nil {
		mAny, _ = m.Update(next())
		m = mAny.(*Model)
	}
	return m
}

// findRow locates the first row whose branch matches and returns the
// index, or -1 if not found.
func findRow(m *Model, branch string) int {
	for i, r := range m.rows {
		if r.wt.Branch == branch {
			return i
		}
	}
	return -1
}

// moveCursor presses j/k until the cursor sits on `target` index.
func moveCursor(t *testing.T, m *Model, target int) *Model {
	t.Helper()
	for m.cursor < target {
		m, _ = keyRune(t, m, 'j')
	}
	for m.cursor > target {
		m, _ = keyRune(t, m, 'k')
	}
	return m
}

func TestTUI_Add_d_y_Removes_Worktree(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()

	if _, err := f.wf.Add(ctx, "repo", "feat"); err != nil {
		t.Fatalf("seed worktree: %v", err)
	}
	if err := f.wf.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	m := f.boot(t)
	m, _ = keyRune(t, m, 'g') // d acts on a worktree, only available in flat view
	idx := findRow(m, "tower/feat")
	if idx < 0 {
		t.Fatalf("tower/feat not on board; rows=%v", branchesOf(m))
	}
	m = moveCursor(t, m, idx)

	m, _ = keyRune(t, m, 'd')
	if m.input != inputConfirmDelete {
		t.Fatalf("expected inputConfirmDelete after d, got %d (err=%v)", m.input, m.err)
	}

	m, cmd := keyRune(t, m, 'y')
	if cmd == nil {
		t.Fatalf("y did not dispatch removeCmd — would manifest to user as 'nothing happens'")
	}
	m = drainCmd(t, m, cmd)
	if m.err != nil {
		t.Fatalf("after remove: err=%v", m.err)
	}
	if findRow(m, "tower/feat") >= 0 {
		t.Fatalf("tower/feat still on board after d/y/load; rows=%v", branchesOf(m))
	}

	out, _ := exec.Command("git", "-C", f.repoPath, "branch").CombinedOutput()
	if strings.Contains(string(out), "tower/feat") {
		t.Fatalf("branch tower/feat still exists in git: %s", out)
	}
	out, _ = exec.Command("git", "-C", f.repoPath, "worktree", "list").CombinedOutput()
	if strings.Contains(string(out), "tower/feat") {
		t.Fatalf("worktree still listed in git: %s", out)
	}
}

func TestTUI_Remove_DirtyWorktree_ForcesViaConfirmation(t *testing.T) {
	// Reproduces the user-reported "git remove worktree refused" error.
	// The sandbox script seeds a dirty worktree on purpose; pressing
	// y on the confirm prompt must pass --force through so dirty
	// worktrees aren't a dead-end in the TUI.
	f := newE2EFixture(t)
	ctx := context.Background()

	wt, err := f.wf.Add(ctx, "repo", "wip")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Make wip dirty: drop an untracked file in it.
	if err := os.WriteFile(filepath.Join(wt.Path, "scratch.txt"), []byte("dirt"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := f.wf.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	m := f.boot(t)
	m, _ = keyRune(t, m, 'g') // d acts on a worktree, only available in flat view
	idx := findRow(m, "tower/wip")
	if idx < 0 {
		t.Fatalf("tower/wip not on board; rows=%v", branchesOf(m))
	}
	if !m.rows[idx].wt.Dirty {
		t.Fatalf("setup mistake: row should be dirty; got %+v", m.rows[idx].wt)
	}
	m = moveCursor(t, m, idx)

	m, _ = keyRune(t, m, 'd')
	if m.input != inputConfirmDelete {
		t.Fatalf("d should open confirm; got input=%d err=%v", m.input, m.err)
	}
	m, cmd := keyRune(t, m, 'y')
	m = drainCmd(t, m, cmd)

	if m.err != nil {
		t.Fatalf("dirty remove should succeed via --force; got err=%v", m.err)
	}
	if findRow(m, "tower/wip") >= 0 {
		t.Fatalf("tower/wip still on board; rows=%v", branchesOf(m))
	}
	// And gone on disk.
	if _, err := os.Stat(wt.Path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("worktree dir should be gone: %v (path=%s)", err, wt.Path)
	}
}

func TestTUI_Remove_KeepsBranch_WhenUnmerged(t *testing.T) {
	f := newE2EFixture(t)
	ctx := context.Background()

	wt, err := f.wf.Add(ctx, "repo", "wip")
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, args := range [][]string{
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-qm", "unmerged work"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = wt.Path
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := f.wf.Reconcile(ctx); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	m := f.boot(t)
	m, _ = keyRune(t, m, 'g') // d acts on a worktree, only available in flat view
	idx := findRow(m, "tower/wip")
	if idx < 0 {
		t.Fatalf("tower/wip not on board")
	}
	m = moveCursor(t, m, idx)
	m, _ = keyRune(t, m, 'd')
	m, cmd := keyRune(t, m, 'y')
	m = drainCmd(t, m, cmd)

	if m.err == nil {
		t.Fatalf("expected ErrBranchKeptUnmerged, got nil err")
	}
	if !strings.Contains(m.err.Error(), "branch kept (unmerged commits)") {
		t.Fatalf("err missing unmerged-commits message: %v", m.err)
	}

	if findRow(m, "tower/wip") >= 0 {
		t.Fatalf("row should be gone after worktree removal; rows=%v", branchesOf(m))
	}
	out, _ := exec.Command("git", "-C", f.repoPath, "branch").CombinedOutput()
	if !strings.Contains(string(out), "tower/wip") {
		t.Fatalf("branch tower/wip should be preserved; got: %s", out)
	}
}

func TestTUI_RegisterRepo_r_Flow(t *testing.T) {
	f := newE2EFixture(t)

	otherRepo := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(otherRepo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-qm", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = otherRepo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v\n%s", err, out)
		}
	}

	m := f.boot(t)
	m, _ = keyRune(t, m, 'r')
	if m.input != inputAddRepoPath {
		t.Fatalf("expected inputAddRepoPath after r; got %d", m.input)
	}
	m = typeRunes(t, m, otherRepo)
	m, cmd := keyEnter(t, m)
	if cmd == nil {
		t.Fatal("enter on r did not dispatch addRepoCmd")
	}
	m = drainCmd(t, m, cmd)
	if m.err != nil {
		t.Fatalf("register err: %v", m.err)
	}
	if m.info == "" {
		t.Fatalf("expected success info, got empty")
	}
	if !strings.Contains(m.info, "registered") {
		t.Fatalf("info should announce registration: %q", m.info)
	}
	repos, _ := f.store.ListRepos(context.Background())
	if len(repos) != 2 {
		t.Fatalf("want 2 repos registered, got %d", len(repos))
	}
}

func TestTUI_AddWorktree_a_Flow(t *testing.T) {
	f := newE2EFixture(t)

	m := f.boot(t)
	if len(m.rows) == 0 {
		t.Fatal("no rows after boot")
	}
	m, _ = keyRune(t, m, 'a')
	if m.input != inputAddName {
		t.Fatalf("expected inputAddName; got %d (err=%v)", m.input, m.err)
	}
	m = typeRunes(t, m, "feat-a")
	m, cmd := keyEnter(t, m)
	m = drainCmd(t, m, cmd)
	if m.err != nil {
		t.Fatalf("add err: %v", m.err)
	}
	if findRow(m, "tower/feat-a") < 0 {
		t.Fatalf("tower/feat-a not on board; rows=%v", branchesOf(m))
	}
}

func TestTUI_RefuseRemoveMainWorktree(t *testing.T) {
	f := newE2EFixture(t)

	m := f.boot(t)
	m, _ = keyRune(t, m, 'g') // d acts on a worktree, only available in flat view
	idx := findRow(m, "main")
	if idx < 0 {
		idx = findRow(m, "master")
	}
	if idx < 0 {
		t.Fatalf("main/master row missing; rows=%v", branchesOf(m))
	}
	mainRow := m.rows[idx]
	t.Logf("main row path=%q", mainRow.wt.Path)
	t.Logf("repo registered path=%q", f.repoPath)

	m = moveCursor(t, m, idx)
	m, _ = keyRune(t, m, 'd')
	if m.input == inputConfirmDelete {
		t.Fatalf("d on main opened confirm prompt instead of refusing; row.path=%q repo.path=%q", mainRow.wt.Path, f.repoPath)
	}
	if m.err == nil {
		t.Fatal("expected error refusing main worktree, got nil")
	}
	if !strings.Contains(m.err.Error(), "main worktree") {
		t.Fatalf("err should mention main worktree: %v", m.err)
	}
}

func TestTUI_AddName_BootstrapsFromOnlyRepo(t *testing.T) {
	repoPath := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repoPath, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-qm", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repoPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v\n%s", err, out)
		}
	}

	dbPath := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()
	s, err := store.OpenSQLite(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	gitFactory := func(p string) observe.Git { return observe.NewGit(p) }
	ghFactory := func(p string) observe.GH { return observe.NewGH(p) }
	ref := refresh.New(s, gitFactory, ghFactory)
	wf := workflow.New(workflow.Config{}, s, gitFactory, ref)
	if _, err := wf.AddRepo(ctx, repoPath, "lonely"); err != nil {
		t.Fatal(err)
	}

	m := newModel(ctx, wf, s)
	mAny, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = mAny.(*Model)
	mAny, _ = m.Update(loadCmd(ctx, wf, s)())
	m = mAny.(*Model)
	if len(m.rows) != 0 {
		t.Fatalf("want 0 rows, got %d", len(m.rows))
	}
	if len(m.repos) != 1 {
		t.Fatalf("want 1 repo loaded, got %d", len(m.repos))
	}

	mAny, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = mAny.(*Model)
	if m.input != inputAddName {
		t.Fatalf("a on empty-board-with-1-repo should open inputAddName; got %d (err=%v)", m.input, m.err)
	}
	if m.inputTarget.wt.Repo != "lonely" {
		t.Fatalf("inputTarget.wt.Repo=%q want lonely", m.inputTarget.wt.Repo)
	}
}

func branchesOf(m *Model) []string {
	out := make([]string, 0, len(m.rows))
	for _, r := range m.rows {
		out = append(out, r.wt.Branch)
	}
	return out
}
