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

// resolveRepoFromFlagOrCwd returns the repo name to operate on. Explicit
// --repo wins; otherwise tower infers from the cwd if it sits inside a
// registered repo.
func resolveRepoFromFlagOrCwd(ctx context.Context, c *cliCtx, repoFlag string) (string, error) {
	if repoFlag != "" {
		return repoFlag, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getwd: %w", err)
	}
	repo, err := c.workflow.RepoForPath(ctx, cwd)
	if err != nil {
		return "", err
	}
	if repo == nil {
		return "", errors.New("no --repo and cwd is not inside a registered repo")
	}
	return repo.Name, nil
}

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repo name (defaults to cwd's repo)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower add <name> [--repo <repo>]")
	}
	name := fs.Arg(0)

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	repoName, err := resolveRepoFromFlagOrCwd(ctx, c, *repoFlag)
	if err != nil {
		return err
	}
	w, err := c.workflow.Add(ctx, repoName, name)
	if err != nil {
		return err
	}
	fmt.Printf("added: %s/%s at %s\n", w.Repo, w.Branch, w.Path)
	return nil
}

func cmdRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repo name (required if name is ambiguous)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower rm <name> [--repo <repo>]")
	}
	name := fs.Arg(0)

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	repoName, err := resolveRepoOrInfer(ctx, c, *repoFlag, name)
	if err != nil {
		return err
	}
	if err := c.workflow.Remove(ctx, repoName, name); err != nil {
		return err
	}
	fmt.Printf("removed: %s/%s\n", repoName, name)
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
	for key, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "  %s: %v\n", key, e)
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
	flat := fs.Bool("flat", false, "list every worktree in one table with a REPO column (default groups by repo)")
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
	repos, err := c.workflow.ListRepos(ctx)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		fmt.Println("no repos registered. run `tower repo add` from a git repo.")
		return nil
	}
	if *flat {
		return printFlat(ctx, c)
	}
	for i, repo := range repos {
		if i > 0 {
			fmt.Println()
		}
		if err := printRepoSection(ctx, c, repo); err != nil {
			return err
		}
	}
	return nil
}

func printFlat(ctx context.Context, c *cliCtx) error {
	worktrees, err := c.workflow.ListWorktrees(ctx)
	if err != nil {
		return err
	}
	if len(worktrees) == 0 {
		fmt.Println("(no worktrees)")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "REPO\tBRANCH\tDIRTY\tA/B\tPR\tCI\tPATH"); err != nil {
		return err
	}
	for _, wt := range worktrees {
		if err := writeFlatRow(ctx, w, c, wt); err != nil {
			return err
		}
	}
	return w.Flush()
}

func writeFlatRow(ctx context.Context, w io.Writer, c *cliCtx, wt domain.Worktree) error {
	pr, err := c.store.GetPullRequest(ctx, wt.Repo, wt.Branch)
	if err != nil {
		return err
	}
	prStr := "-"
	ciStr := "-"
	if pr != nil {
		prStr = fmt.Sprintf("#%d %s", pr.Number, pr.State)
		checks, err := c.store.ListCIChecks(ctx, wt.Repo, pr.Number)
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
	_, err = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		wt.Repo, wt.Branch, dirty, ab, prStr, ciStr, wt.Path)
	return err
}

func printRepoSection(ctx context.Context, c *cliCtx, repo domain.Repo) error {
	fmt.Println(repo.Name)
	worktrees, err := c.store.ListWorktreesForRepo(ctx, repo.Name)
	if err != nil {
		return err
	}
	if len(worktrees) == 0 {
		fmt.Println("  (no worktrees)")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "  BRANCH\tDIRTY\tA/B\tPR\tCI\tPATH"); err != nil {
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
	pr, err := c.store.GetPullRequest(ctx, wt.Repo, wt.Branch)
	if err != nil {
		return err
	}
	prStr := "-"
	ciStr := "-"
	if pr != nil {
		prStr = fmt.Sprintf("#%d %s", pr.Number, pr.State)
		checks, err := c.store.ListCIChecks(ctx, wt.Repo, pr.Number)
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
	_, err = fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\n",
		wt.Branch, dirty, ab, prStr, ciStr, wt.Path)
	return err
}

func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repo name (required if name is ambiguous)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower open <name> [--repo <repo>]")
	}
	name := fs.Arg(0)

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	wt, err := c.workflow.Resolve(ctx, *repoFlag, name)
	if err != nil {
		return err
	}
	if wt == nil {
		return fmt.Errorf("no worktree found for %q", name)
	}
	fmt.Println(wt.Path)
	return nil
}
