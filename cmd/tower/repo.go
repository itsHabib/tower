package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"text/tabwriter"
)

func cmdRepo(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: tower repo <add|ls|rm|prune> [...]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return cmdRepoAdd(rest)
	case "ls":
		return cmdRepoLs(rest)
	case "rm":
		return cmdRepoRm(rest)
	case "prune":
		return cmdRepoPrune(rest)
	default:
		return fmt.Errorf("unknown repo subcommand: %s", sub)
	}
}

func cmdRepoAdd(args []string) error {
	fset := flag.NewFlagSet("repo add", flag.ExitOnError)
	name := fset.String("name", "", "repo name (defaults to directory basename)")
	if err := fset.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	path := ""
	if fset.NArg() > 0 {
		path = fset.Arg(0)
	}
	if path == "" {
		top, err := gitTopLevel(ctx)
		if err != nil {
			return fmt.Errorf("infer repo from cwd: %w", err)
		}
		path = top
	}
	return runRepoAdd(ctx, c, path, *name, os.Stdout)
}

func runRepoAdd(ctx context.Context, c *cliCtx, path, name string, out io.Writer) error {
	r, err := c.workflow.AddRepo(ctx, path, name)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "registered: %s at %s\n", r.Name, r.Path)
	return err
}

func cmdRepoLs(args []string) error {
	fset := flag.NewFlagSet("repo ls", flag.ExitOnError)
	if err := fset.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return runRepoLs(ctx, c, os.Stdout)
}

func runRepoLs(ctx context.Context, c *cliCtx, out io.Writer) error {
	repos, err := c.workflow.ListRepos(ctx)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		_, err := fmt.Fprintln(out, "no repos registered. run `tower repo add` from a git repo.")
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "NAME\tPATH"); err != nil {
		return err
	}
	for _, r := range repos {
		if _, err := fmt.Fprintf(w, "%s\t%s\n", r.Name, r.Path); err != nil {
			return err
		}
	}
	return w.Flush()
}

func cmdRepoRm(args []string) error {
	fset := flag.NewFlagSet("repo rm", flag.ExitOnError)
	if err := fset.Parse(args); err != nil {
		return err
	}
	if fset.NArg() < 1 {
		return errors.New("usage: tower repo rm <name>")
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return runRepoRm(ctx, c, fset.Arg(0), os.Stdout)
}

func runRepoRm(ctx context.Context, c *cliCtx, name string, out io.Writer) error {
	if err := c.workflow.RemoveRepo(ctx, name); err != nil {
		return err
	}
	_, err := fmt.Fprintf(out, "unregistered: %s\n", name)
	return err
}

func cmdRepoPrune(args []string) error {
	fset := flag.NewFlagSet("repo prune", flag.ExitOnError)
	dryRun := fset.Bool("dry-run", false, "report what would be removed without removing")
	if err := fset.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return runRepoPrune(ctx, c, *dryRun, os.Stdout)
}

func runRepoPrune(ctx context.Context, c *cliCtx, dryRun bool, out io.Writer) error {
	repos, err := c.workflow.ListRepos(ctx)
	if err != nil {
		return err
	}
	missing := make([]string, 0, len(repos))
	for _, r := range repos {
		if _, statErr := os.Stat(r.Path); errors.Is(statErr, fs.ErrNotExist) {
			missing = append(missing, r.Name)
		}
	}
	if len(missing) == 0 {
		_, err := fmt.Fprintln(out, "nothing to prune.")
		return err
	}
	if dryRun {
		if _, err := fmt.Fprintf(out, "would remove %d repo(s):\n", len(missing)); err != nil {
			return err
		}
		for _, name := range missing {
			if _, err := fmt.Fprintf(out, "  %s\n", name); err != nil {
				return err
			}
		}
		return nil
	}
	for _, name := range missing {
		if err := c.workflow.RemoveRepo(ctx, name); err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}
		if _, err := fmt.Fprintf(out, "pruned: %s\n", name); err != nil {
			return err
		}
	}
	return nil
}
