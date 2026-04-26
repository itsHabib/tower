package observe

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// GitObserver implements Git by shelling out to the local git binary.
type GitObserver struct {
	Repo   string
	Runner Runner
}

// NewGit returns a GitObserver rooted at repoRoot, using ExecRunner.
func NewGit(repoRoot string) *GitObserver {
	return &GitObserver{Repo: repoRoot, Runner: ExecRunner{}}
}

// Worktrees lists every worktree attached to the repository.
func (g *GitObserver) Worktrees(ctx context.Context) ([]Worktree, error) {
	out, err := g.Runner.Run(ctx, g.Repo, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	return parseWorktreeList(out)
}

// AddWorktree creates a new worktree at path on a fresh branch.
func (g *GitObserver) AddWorktree(ctx context.Context, path, branch string) error {
	if _, err := g.Runner.Run(ctx, g.Repo, "git", "worktree", "add", "-b", branch, path); err != nil {
		return fmt.Errorf("git worktree add: %w", err)
	}
	return nil
}

// RemoveWorktree tears down the worktree at path.
func (g *GitObserver) RemoveWorktree(ctx context.Context, path string) error {
	if _, err := g.Runner.Run(ctx, g.Repo, "git", "worktree", "remove", path); err != nil {
		return fmt.Errorf("git worktree remove: %w", err)
	}
	return nil
}

// Dirty reports whether the worktree at path has uncommitted changes.
func (g *GitObserver) Dirty(ctx context.Context, path string) (bool, error) {
	out, err := g.Runner.Run(ctx, path, "git", "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("git status: %w", err)
	}
	return len(bytes.TrimSpace(out)) > 0, nil
}

// AheadBehind returns commits ahead and behind the worktree's upstream.
// Tries @{u} first, falls back to origin/HEAD; returns (0, 0, nil) if neither resolves.
func (g *GitObserver) AheadBehind(ctx context.Context, path string) (int, int, error) {
	for _, base := range []string{"@{u}", "origin/HEAD"} {
		a, b, ok := g.tryAheadBehind(ctx, path, base)
		if ok {
			return a, b, nil
		}
	}
	return 0, 0, nil
}

func (g *GitObserver) tryAheadBehind(ctx context.Context, path, base string) (int, int, bool) {
	out, err := g.Runner.Run(ctx, path, "git", "rev-list", "--left-right", "--count", base+"...HEAD")
	if err != nil {
		return 0, 0, false
	}
	parts := strings.Fields(string(out))
	if len(parts) != 2 {
		return 0, 0, false
	}
	behind, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, false
	}
	ahead, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, false
	}
	return ahead, behind, true
}

// LastCommit returns the timestamp and subject of HEAD for the worktree at path.
func (g *GitObserver) LastCommit(ctx context.Context, path string) (time.Time, string, error) {
	out, err := g.Runner.Run(ctx, path, "git", "log", "-1", "--format=%ct%n%s")
	if err != nil {
		return time.Time{}, "", fmt.Errorf("git log: %w", err)
	}
	lines := strings.SplitN(strings.TrimSpace(string(out)), "\n", 2)
	if len(lines) == 0 || lines[0] == "" {
		return time.Time{}, "", nil
	}
	ts, err := strconv.ParseInt(lines[0], 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("parse timestamp %q: %w", lines[0], err)
	}
	subject := ""
	if len(lines) > 1 {
		subject = lines[1]
	}
	return time.Unix(ts, 0).UTC(), subject, nil
}

// MainRoot returns the absolute path of the main worktree of the repo.
// The first entry from `git worktree list --porcelain` is always the main worktree.
func (g *GitObserver) MainRoot(ctx context.Context) (string, error) {
	wts, err := g.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	if len(wts) == 0 {
		return "", errors.New("no worktrees found")
	}
	return wts[0].Path, nil
}

func parseWorktreeList(data []byte) ([]Worktree, error) {
	var out []Worktree
	var cur Worktree
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if cur.Path != "" {
				out = append(out, cur)
			}
			cur = Worktree{}
			continue
		}
		switch {
		case strings.HasPrefix(line, "worktree "):
			cur.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			cur.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	if cur.Path != "" {
		out = append(out, cur)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
