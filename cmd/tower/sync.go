package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
)

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
	return runSync(ctx, c, os.Stdout, os.Stderr)
}

func runSync(ctx context.Context, c *cliCtx, out, errOut io.Writer) error {
	res, err := c.workflow.Sync(ctx)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "sync: %d ok, %d errors\n", res.Synced, len(res.Errors)); err != nil {
		return err
	}
	for key, e := range res.Errors {
		if _, err := fmt.Fprintf(errOut, "  %s: %v\n", key, e); err != nil {
			return err
		}
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
	return runReconcile(ctx, c, os.Stdout)
}

func runReconcile(ctx context.Context, c *cliCtx, out io.Writer) error {
	if err := c.workflow.Reconcile(ctx); err != nil {
		return err
	}
	_, err := fmt.Fprintln(out, "reconcile: ok")
	return err
}
