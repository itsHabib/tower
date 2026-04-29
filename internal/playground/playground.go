// Package playground builds a synthetic multi-repo / multi-worktree
// fixture on disk and registers it with a workflow.Service. Used by
// scripts/seed (manual TUI sandbox) and by integration tests that
// want a realistic-shaped board without hand-rolling git operations.
package playground

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/itsHabib/tower/internal/workflow"
)

// Repo describes one fake repo in a fixture.
type Repo struct {
	Name      string
	Worktrees []Worktree
}

// Worktree describes one worktree inside a fixture repo. BranchPrefix
// defaults to "tower/" when empty. ExtraCommits are made on the branch
// after creation so the A/B column has data; Dirty leaves an
// uncommitted file in the working tree.
type Worktree struct {
	Name         string
	BranchPrefix string
	Dirty        bool
	ExtraCommits int
}

// Default is the playground used by scripts/seed: 6 repos, 23 total
// worktrees, mixed clean / dirty / ahead. Tests can pass smaller
// fixtures.
//
// Repo names are Greek letters on purpose — they're obviously
// throwaway and can't ever collide with a real repo name. Don't
// rename them to anything that looks production.
var Default = []Repo{
	{Name: "alpha", Worktrees: []Worktree{
		{Name: "feat-one", BranchPrefix: "tower/", ExtraCommits: 2},
		{Name: "feat-two", BranchPrefix: "tower/", Dirty: true, ExtraCommits: 1},
		{Name: "bugfix", BranchPrefix: "bug/", ExtraCommits: 1},
		{Name: "rotation", BranchPrefix: "feat/", Dirty: true, ExtraCommits: 3},
		{Name: "spike", BranchPrefix: "experimental/", ExtraCommits: 5},
	}},
	{Name: "beta", Worktrees: []Worktree{
		{Name: "runner", BranchPrefix: "tower/", ExtraCommits: 1},
		{Name: "store", BranchPrefix: "tower/", Dirty: true, ExtraCommits: 4},
		{Name: "metrics", BranchPrefix: "tower/"},
		{Name: "tracing", BranchPrefix: "feat/", ExtraCommits: 2},
	}},
	{Name: "gamma", Worktrees: []Worktree{
		{Name: "retries", BranchPrefix: "tower/", ExtraCommits: 3},
		{Name: "validator", BranchPrefix: "tower/", Dirty: true},
		{Name: "fanout", BranchPrefix: "feat/", ExtraCommits: 1},
	}},
	{Name: "delta", Worktrees: []Worktree{
		{Name: "planner", BranchPrefix: "tower/", ExtraCommits: 6},
		{Name: "cache", BranchPrefix: "tower/", Dirty: true, ExtraCommits: 2},
		{Name: "retry", BranchPrefix: "feat/"},
		{Name: "debug", BranchPrefix: "wip/", Dirty: true},
		{Name: "validate", BranchPrefix: "feat/", ExtraCommits: 1},
	}},
	{Name: "epsilon", Worktrees: []Worktree{
		{Name: "router", BranchPrefix: "tower/", ExtraCommits: 8},
		{Name: "checks", BranchPrefix: "tower/", Dirty: true, ExtraCommits: 1},
		{Name: "replay", BranchPrefix: "feat/"},
		{Name: "precision", BranchPrefix: "bug/", ExtraCommits: 2},
	}},
	{Name: "zeta", Worktrees: []Worktree{
		{Name: "tokens", BranchPrefix: "tower/", ExtraCommits: 1},
		{Name: "rewrite", BranchPrefix: "tower/", Dirty: true, ExtraCommits: 2},
	}},
}

// Result is the summary returned from Seed.
type Result struct {
	Repos     int
	Worktrees int
}

// ProgressFn receives a one-line human-readable update each time Seed
// finishes a meaningful step (repo init, worktree creation, final
// reconcile). nil = silent. Lets callers print or log progress so 20+
// seconds of git ops don't look like a freeze.
type ProgressFn func(msg string)

// Seed builds the fixture under repoRoot and registers each repo with
// wf. Calls wf.Reconcile at the end so non-tower-prefixed worktrees
// land in the store with up-to-date metadata. progress may be nil.
func Seed(ctx context.Context, wf *workflow.Service, repoRoot string, fixture []Repo, progress ProgressFn) (Result, error) {
	if progress == nil {
		progress = func(string) {}
	}
	if err := os.MkdirAll(repoRoot, 0o755); err != nil {
		return Result{}, err
	}
	res := Result{Repos: len(fixture)}
	for _, r := range fixture {
		progress(fmt.Sprintf("  %-15s seeding %d worktrees…", r.Name, len(r.Worktrees)))
		repoPath := filepath.Join(repoRoot, r.Name)
		if err := os.MkdirAll(repoPath, 0o755); err != nil {
			return res, err
		}
		if err := initRepo(ctx, repoPath); err != nil {
			return res, fmt.Errorf("init %s: %w", r.Name, err)
		}
		if _, err := wf.AddRepo(ctx, repoPath, r.Name); err != nil {
			return res, fmt.Errorf("register %s: %w", r.Name, err)
		}
		for _, wt := range r.Worktrees {
			if err := seedWorktree(ctx, wf, r.Name, repoPath, wt); err != nil {
				return res, fmt.Errorf("worktree %s/%s: %w", r.Name, wt.Name, err)
			}
		}
		res.Worktrees += len(r.Worktrees)
	}
	progress("reconciling worktree state…")
	if err := wf.Reconcile(ctx); err != nil {
		return res, fmt.Errorf("reconcile: %w", err)
	}
	return res, nil
}

func initRepo(ctx context.Context, dir string) error {
	steps := [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "sandbox@tower"},
		{"config", "user.name", "sandbox"},
	}
	for _, args := range steps {
		if err := runGit(ctx, dir, args...); err != nil {
			return err
		}
	}
	if err := writeFile(filepath.Join(dir, "README.md"), "# "+filepath.Base(dir)+"\n"); err != nil {
		return err
	}
	if err := runGit(ctx, dir, "add", "README.md"); err != nil {
		return err
	}
	if err := runGit(ctx, dir, "commit", "-qm", "initial"); err != nil {
		return err
	}
	for i := 1; i <= 3; i++ {
		f := fmt.Sprintf("main-%d.txt", i)
		if err := writeFile(filepath.Join(dir, f), fmt.Sprintf("seed %d\n", i)); err != nil {
			return err
		}
		if err := runGit(ctx, dir, "add", f); err != nil {
			return err
		}
		if err := runGit(ctx, dir, "commit", "-qm", fmt.Sprintf("main update %d", i)); err != nil {
			return err
		}
	}
	return nil
}

func seedWorktree(ctx context.Context, wf *workflow.Service, repoName, repoPath string, wt Worktree) error {
	prefix := wt.BranchPrefix
	if prefix == "" {
		prefix = "tower/"
	}
	branch := prefix + wt.Name
	wtPath := filepath.Join(repoPath, ".worktrees", wt.Name)

	if prefix == "tower/" {
		if _, err := wf.Add(ctx, repoName, wt.Name); err != nil {
			return err
		}
	} else {
		if err := runGit(ctx, repoPath, "worktree", "add", "-q", "-b", branch, wtPath); err != nil {
			return err
		}
	}

	if err := runGit(ctx, wtPath, "branch", "--set-upstream-to=main", branch); err != nil {
		return err
	}

	for i := 0; i < wt.ExtraCommits; i++ {
		f := fmt.Sprintf("commit-%d.txt", i+1)
		if err := writeFile(filepath.Join(wtPath, f), fmt.Sprintf("change %d\n", i+1)); err != nil {
			return err
		}
		if err := runGit(ctx, wtPath, "add", f); err != nil {
			return err
		}
		msg := fmt.Sprintf("%s: change %d", branch, i+1)
		if err := runGit(ctx, wtPath, "commit", "-qm", msg); err != nil {
			return err
		}
	}
	if wt.Dirty {
		if err := writeFile(filepath.Join(wtPath, "scratch.txt"), "uncommitted local edit\n"); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o644)
}

// runGit shells out to git from dir with the given args. The seeder
// only ever invokes git, so we don't bother parameterising the program
// name (lint catches that as a dead arg).
func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return nil
}
