package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/tui"
)

type lsOpts struct {
	noReconcile bool
	flat        bool
	json        bool
	sort        tui.SortMode
}

func cmdLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ExitOnError)
	noReconcile := fs.Bool("no-reconcile", false, "skip the git reconcile pass before listing")
	flat := fs.Bool("flat", false, "list every worktree in one table with a REPO column (default groups by repo)")
	jsonOut := fs.Bool("json", false, "emit a flat JSON array of rows instead of a table")
	sortFlag := fs.String("sort", "attention", "row order: attention | activity | name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	mode, err := tui.ParseSortMode(*sortFlag)
	if err != nil {
		return err
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return runLs(ctx, c, lsOpts{
		noReconcile: *noReconcile,
		flat:        *flat,
		json:        *jsonOut,
		sort:        mode,
	}, os.Stdout)
}

func runLs(ctx context.Context, c *cliCtx, opts lsOpts, out io.Writer) error {
	if !opts.noReconcile {
		if err := c.workflow.Reconcile(ctx); err != nil {
			return fmt.Errorf("reconcile: %w", err)
		}
	}
	if opts.json {
		return runLsJSON(ctx, c, opts.sort, out)
	}
	repos, err := c.workflow.ListRepos(ctx)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		_, err := fmt.Fprintln(out, "no repos registered yet.\n\n  cd <repo> && tower repo add")
		return err
	}
	if opts.flat {
		return runLsFlat(ctx, c, opts.sort, out)
	}
	for i, repo := range repos {
		if i > 0 {
			if _, err := fmt.Fprintln(out); err != nil {
				return err
			}
		}
		if err := runLsRepoSection(ctx, c, repo, opts.sort, out); err != nil {
			return err
		}
	}
	return nil
}

func runLsJSON(ctx context.Context, c *cliCtx, mode tui.SortMode, out io.Writer) error {
	worktrees, err := c.workflow.ListWorktrees(ctx)
	if err != nil {
		return err
	}
	rows, err := buildRows(ctx, c, worktrees)
	if err != nil {
		return err
	}
	tui.SortRowData(rows, mode)
	return writeJSON(out, rows)
}

func writeJSON(w io.Writer, rows []tui.RowData) error {
	out := make([]domain.WorktreeView, len(rows))
	for i, r := range rows {
		reviews := r.Reviews
		if reviews == nil {
			reviews = []domain.Review{}
		}
		checks := r.Checks
		if checks == nil {
			checks = []domain.CICheck{}
		}
		out[i] = domain.WorktreeView{Worktree: r.Worktree, PR: r.PR, Reviews: reviews, Checks: checks}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func runLsFlat(ctx context.Context, c *cliCtx, mode tui.SortMode, out io.Writer) error {
	worktrees, err := c.workflow.ListWorktrees(ctx)
	if err != nil {
		return err
	}
	if len(worktrees) == 0 {
		_, err := fmt.Fprintln(out, "(no worktrees)")
		return err
	}
	rows, err := buildRows(ctx, c, worktrees)
	if err != nil {
		return err
	}
	tui.SortRowData(rows, mode)
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "REPO\tBRANCH\tDIRTY\tA/B\tPR\tCI\tLAST\tPATH"); err != nil {
		return err
	}
	for _, r := range rows {
		if err := writeFlatRowData(w, r); err != nil {
			return err
		}
	}
	return w.Flush()
}

func writeFlatRowData(w io.Writer, r tui.RowData) error {
	wt := r.Worktree
	prStr := "-"
	ciStr := "-"
	if r.PR != nil {
		prStr = fmt.Sprintf("#%d %s", r.PR.Number, r.PR.State)
		ciStr = tui.SummarizeChecks(r.Checks)
	}
	dirty := "-"
	if wt.Dirty {
		dirty = "yes"
	}
	ab := fmt.Sprintf("%d/%d", wt.Ahead, wt.Behind)
	last := lastSummary(wt)
	_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		wt.Repo, wt.Branch, dirty, ab, prStr, ciStr, last, wt.Path)
	return err
}

func runLsRepoSection(ctx context.Context, c *cliCtx, repo domain.Repo, mode tui.SortMode, out io.Writer) error {
	if _, err := fmt.Fprintln(out, repo.Name); err != nil {
		return err
	}
	worktrees, err := c.store.ListWorktreesForRepo(ctx, repo.Name)
	if err != nil {
		return err
	}
	if len(worktrees) == 0 {
		_, err := fmt.Fprintln(out, "  (no worktrees)")
		return err
	}
	rows, err := buildRows(ctx, c, worktrees)
	if err != nil {
		return err
	}
	tui.SortRowData(rows, mode)
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "  BRANCH\tDIRTY\tA/B\tPR\tCI\tLAST\tPATH"); err != nil {
		return err
	}
	for _, r := range rows {
		if err := writeWorktreeRowData(w, r); err != nil {
			return err
		}
	}
	return w.Flush()
}

// buildRows hydrates each worktree with its PR, reviews, and checks.
func buildRows(ctx context.Context, c *cliCtx, worktrees []domain.Worktree) ([]tui.RowData, error) {
	rows := make([]tui.RowData, 0, len(worktrees))
	for _, wt := range worktrees {
		pr, err := c.store.GetPullRequest(ctx, wt.Repo, wt.Branch)
		if err != nil {
			return nil, err
		}
		var (
			reviews []domain.Review
			checks  []domain.CICheck
		)
		if pr != nil {
			reviews, err = c.store.ListReviews(ctx, wt.Repo, pr.Number)
			if err != nil {
				return nil, err
			}
			checks, err = c.store.ListCIChecks(ctx, wt.Repo, pr.Number)
			if err != nil {
				return nil, err
			}
		}
		rows = append(rows, tui.RowData{
			Worktree: wt, PR: pr, Reviews: reviews, Checks: checks,
			Priority: tui.RowPriority(wt, pr, reviews, checks),
		})
	}
	return rows, nil
}

func writeWorktreeRowData(w io.Writer, r tui.RowData) error {
	wt := r.Worktree
	prStr := "-"
	ciStr := "-"
	if r.PR != nil {
		prStr = fmt.Sprintf("#%d %s", r.PR.Number, r.PR.State)
		ciStr = tui.SummarizeChecks(r.Checks)
	}
	dirty := "-"
	if wt.Dirty {
		dirty = "yes"
	}
	ab := fmt.Sprintf("%d/%d", wt.Ahead, wt.Behind)
	last := lastSummary(wt)
	_, err := fmt.Fprintf(w, "  %s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		wt.Branch, dirty, ab, prStr, ciStr, last, wt.Path)
	return err
}

func lastSummary(wt domain.Worktree) string {
	age := tui.FormatAge(wt.LastCommit)
	switch {
	case age == "" && wt.Title == "":
		return "-"
	case age == "":
		return wt.Title
	case wt.Title == "":
		return age
	default:
		return age + " · " + wt.Title
	}
}
