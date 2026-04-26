package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
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
	r, err := c.workflow.AddRepo(ctx, path, *name)
	if err != nil {
		return err
	}
	fmt.Printf("registered: %s at %s\n", r.Name, r.Path)
	return nil
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

	repos, err := c.workflow.ListRepos(ctx)
	if err != nil {
		return err
	}
	if len(repos) == 0 {
		fmt.Println("no repos registered. run `tower repo add` from a git repo.")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
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
	name := fset.Arg(0)
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	if err := c.workflow.RemoveRepo(ctx, name); err != nil {
		return err
	}
	fmt.Printf("unregistered: %s\n", name)
	return nil
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
		fmt.Println("nothing to prune.")
		return nil
	}
	if *dryRun {
		fmt.Printf("would remove %d repo(s):\n", len(missing))
		for _, name := range missing {
			fmt.Printf("  %s\n", name)
		}
		return nil
	}
	for _, name := range missing {
		if err := c.workflow.RemoveRepo(ctx, name); err != nil {
			return fmt.Errorf("remove %s: %w", name, err)
		}
		fmt.Printf("pruned: %s\n", name)
	}
	return nil
}
