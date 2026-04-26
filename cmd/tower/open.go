package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/itsHabib/tower/internal/tui"
)

func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repo name (required if name is ambiguous)")
	prFlag := fs.Bool("pr", false, "open the PR for this worktree in your browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower open <name> [--repo <repo>] [--pr]")
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return runOpen(ctx, c, fs.Arg(0), *repoFlag, *prFlag, os.Stdout)
}

func runOpen(ctx context.Context, c *cliCtx, name, repoFlag string, openPR bool, out io.Writer) error {
	wt, err := c.workflow.Resolve(ctx, repoFlag, name)
	if err != nil {
		return err
	}
	if wt == nil {
		return fmt.Errorf("no worktree found for %q", name)
	}
	if openPR {
		pr, err := c.store.GetPullRequest(ctx, wt.Repo, wt.Branch)
		if err != nil {
			return err
		}
		if pr == nil || pr.URL == "" {
			return fmt.Errorf("no PR tracked for %s/%s (try `tower sync` first)", wt.Repo, wt.Branch)
		}
		return tui.OpenInBrowser(ctx, pr.URL)
	}
	_, err = fmt.Fprintln(out, wt.Path)
	return err
}
