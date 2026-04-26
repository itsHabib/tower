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
	"text/tabwriter"

	"github.com/itsHabib/tower/internal/domain"
)

func cmdDiscover(args []string) error {
	fs := flag.NewFlagSet("discover", flag.ExitOnError)
	dir := fs.String("d", "features", "directory to scan for task markdown files")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	target := *dir
	if !filepath.IsAbs(target) {
		target = filepath.Join(c.repo, target)
	}
	res, err := c.workflow.Discover(ctx, target)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	fmt.Printf("discover: %d added, %d updated, %d total in %s\n",
		res.Added, res.Updated, len(res.Tasks), target)
	return nil
}

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower add <task-id>")
	}
	taskID := fs.Arg(0)

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := c.workflow.Add(ctx, taskID); err != nil {
		return err
	}
	wt, err := c.store.GetWorktree(ctx, taskID)
	if err != nil {
		return err
	}
	fmt.Printf("added: worktree at %s on branch %s\n", wt.Path, wt.Branch)
	return nil
}

func cmdRm(args []string) error {
	fs := flag.NewFlagSet("rm", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower rm <task-id>")
	}
	taskID := fs.Arg(0)

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if err := c.workflow.Remove(ctx, taskID); err != nil {
		return err
	}
	fmt.Printf("removed: %s\n", taskID)
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
	for id, e := range res.Errors {
		fmt.Fprintf(os.Stderr, "  %s: %v\n", id, e)
	}
	return nil
}

func cmdLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	tasks, err := c.store.ListTasks(ctx)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Println("no tasks")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "ID\tSTATUS\tPR\tCI\tWORKTREE"); err != nil {
		return err
	}
	for _, t := range tasks {
		if err := writeTaskRow(ctx, w, c, t); err != nil {
			return err
		}
	}
	return w.Flush()
}

func writeTaskRow(ctx context.Context, w io.Writer, c *cliCtx, t domain.Task) error {
	wt, err := c.store.GetWorktree(ctx, t.ID)
	if err != nil {
		return err
	}
	pr, err := c.store.GetPullRequest(ctx, t.ID)
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
		ciStr = summarizeChecks(checks)
	}
	wtStr := "-"
	if wt != nil {
		wtStr = wt.Path
	}
	_, err = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.Status, prStr, ciStr, wtStr)
	return err
}

func summarizeChecks(checks []domain.CICheck) string {
	if len(checks) == 0 {
		return "-"
	}
	counts := map[domain.CIConclusion]int{}
	for _, c := range checks {
		counts[c.Conclusion]++
	}
	order := []domain.CIConclusion{
		domain.CISuccess, domain.CIFailure, domain.CIPending,
		domain.CISkipped, domain.CICanceled,
	}
	parts := make([]string, 0, len(order))
	for _, conc := range order {
		if counts[conc] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[conc], conc))
		}
	}
	return strings.Join(parts, " ")
}

func cmdOpen(args []string) error {
	fs := flag.NewFlagSet("open", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower open <task-id>")
	}
	taskID := fs.Arg(0)

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	wt, err := c.store.GetWorktree(ctx, taskID)
	if err != nil {
		return err
	}
	if wt == nil {
		return fmt.Errorf("no worktree for %s", taskID)
	}
	fmt.Println(wt.Path)
	return nil
}
