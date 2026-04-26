package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
)

func cmdRepo(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: tower repo <add|ls|rm> [...]")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return cmdRepoAdd(rest)
	case "ls":
		return cmdRepoLs(rest)
	case "rm":
		return cmdRepoRm(rest)
	default:
		return fmt.Errorf("unknown repo subcommand: %s", sub)
	}
}

func cmdRepoAdd(args []string) error {
	fs := flag.NewFlagSet("repo add", flag.ExitOnError)
	name := fs.String("name", "", "repo name (defaults to directory basename)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	path := ""
	if fs.NArg() > 0 {
		path = fs.Arg(0)
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
	fs := flag.NewFlagSet("repo ls", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
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
	fs := flag.NewFlagSet("repo rm", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower repo rm <name>")
	}
	name := fs.Arg(0)
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
