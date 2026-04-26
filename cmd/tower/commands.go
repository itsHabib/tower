package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/tui"
)

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower add <name>")
	}
	name := fs.Arg(0)

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	w, err := c.workflow.Add(ctx, name)
	if err != nil {
		return err
	}
	fmt.Printf("added: worktree at %s on branch %s\n", w.Path, w.Branch)
	return nil
}

func cmdRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower rm <name>")
	}
	name := fs.Arg(0)

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := c.workflow.Remove(ctx, name); err != nil {
		return err
	}
	fmt.Printf("removed: %s\n", name)
	return nil
}

func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	res, err := c.workflow.Sync(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("sync: %d ok, %d errors\n", res.Synced, len(res.Errors))
	for branch, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "  %s: %v\n", branch, e)
	}
	return nil
}

func cmdReconcile(args []string) error {
	fs := flag.NewFlagSet("reconcile", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := c.workflow.Reconcile(ctx); err != nil {
		return err
	}
	fmt.Println("reconcile: ok")
	return nil
}

func cmdLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	noReconcile := fs.Bool("no-reconcile", false, "skip the git reconcile pass before listing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if !*noReconcile {
		if err := c.workflow.Reconcile(ctx); err != nil {
			return fmt.Errorf("reconcile: %w", err)
		}
	}
	worktrees, err := c.store.ListWorktrees(ctx)
	if err != nil {
		return err
	}
	if len(worktrees) == 0 {
		fmt.Println("no worktrees tracked")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "BRANCH\tDIRTY\tA/B\tPR\tCI\tPATH"); err != nil {
		return err
	}
	for _, wt := range worktrees {
		if err := writeWorktreeRow(ctx, w, c, wt); err != nil {
			return err
		}
	}
	return w.Flush()
}

func writeWorktreeRow(ctx context.Context, w io.Writer, c *cliCtx, wt domain.Worktree) error {
	pr, err := c.store.GetPullRequest(ctx, wt.Branch)
	if err != nil {
		return err
	}
	prStr := "-"
	ciStr := "-"
	if pr != nil {
		prStr = fmt.Sprintf("#%d %s", pr.Number, pr.State)
		checks, err := c.store.ListCIChecks(ctx, pr.Number)
		if err != nil {
			return err
		}
		ciStr = tui.SummarizeChecks(checks)
	}
	dirty := "-"
	if wt.Dirty {
		dirty = "yes"
	}
	ab := fmt.Sprintf("%d/%d", wt.Ahead, wt.Behind)
	_, err = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
		wt.Branch, dirty, ab, prStr, ciStr, wt.Path)
	return err
}

func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower open <name>")
	}
	name := fs.Arg(0)

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	wt, err := c.workflow.Resolve(ctx, name)
	if err != nil {
		return err
	}
	if wt == nil {
		return fmt.Errorf("no worktree for %s", name)
	}
	fmt.Println(wt.Path)
	return nil
}
