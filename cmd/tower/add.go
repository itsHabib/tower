package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
)

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ExitOnError)
	repoFlag := fs.String("repo", "", "repo name (defaults to cwd's repo)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower add <name> [--repo <repo>]")
	}
	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()
	return runAdd(ctx, c, fs.Arg(0), *repoFlag, os.Stdout)
}

func runAdd(ctx context.Context, c *cliCtx, name, repoFlag string, out io.Writer) error {
	repoName, err := resolveRepoFromFlagOrCwd(ctx, c, repoFlag)
	if err != nil {
		return err
	}
	w, err := c.workflow.Add(ctx, repoName, name)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "added: %s/%s at %s\n", w.Repo, w.Branch, w.Path)
	return err
}

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
