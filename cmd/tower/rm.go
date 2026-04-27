package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func cmdRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repo name (required if name is ambiguous)")
	force := fs.Bool("force", false, "remove the worktree even if it has uncommitted changes (passes --force to git)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower rm <name> [--repo <repo>] [--force]")
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return runRm(ctx, c, fs.Arg(0), *repoFlag, *force, os.Stdout)
}

func runRm(ctx context.Context, c *cliCtx, name, repoFlag string, force bool, out io.Writer) error {
	repoName, err := resolveRepoOrInfer(ctx, c, repoFlag, name)
	if err != nil {
		return err
	}
	wt, err := c.workflow.Resolve(ctx, repoName, name)
	if err != nil {
		return err
	}
	if wt == nil {
		return fmt.Errorf("no worktree tracked for %s/%s", repoName, name)
	}
	if err := refuseIfCwdInside(wt.Path); err != nil {
		return err
	}
	if err := c.workflow.Remove(ctx, repoName, name, force); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "removed: %s/%s\n", repoName, name)
	return err
}

// refuseIfCwdInside errors out if the current working directory is at or
// below target. On Windows git worktree remove fails with "permission
// denied" in this case because the shell holds an open handle to the dir.
func refuseIfCwdInside(target string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return nil //nolint:nilerr // best-effort safety check: if cwd is unreadable, let git surface its own error
	}
	cwd = filepath.Clean(cwd)
	target = filepath.Clean(target)
	if cwd == target || strings.HasPrefix(cwd, target+string(filepath.Separator)) {
		return fmt.Errorf("refusing to remove %s: your shell is currently inside it. cd elsewhere first", target)
	}
	return nil
}

// resolveRepoOrInfer picks a repo for an action: --repo wins, then cwd,
// then a unique branch match across all repos. Errors if ambiguous.
func resolveRepoOrInfer(ctx context.Context, c *cliCtx, repoFlag, name string) (string, error) {
	if repoFlag != "" {
		return repoFlag, nil
	}
	if cwd, err := os.Getwd(); err == nil {
		if repo, err := c.workflow.RepoForPath(ctx, cwd); err == nil && repo != nil {
			return repo.Name, nil
		}
	}
	wt, err := c.workflow.Resolve(ctx, "", name)
	if err != nil {
		return "", err
	}
	if wt == nil {
		return "", fmt.Errorf("no worktree found for %q in any registered repo", name)
	}
	return wt.Repo, nil
}
