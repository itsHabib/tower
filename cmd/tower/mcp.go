package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	towermcp "github.com/itsHabib/tower/internal/mcp"
)

func cmdMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("usage: tower mcp serve")
	}
	sub := fs.Arg(0)
	if sub != "serve" {
		return fmt.Errorf("unknown mcp subcommand: %s", sub)
	}

	ctx := context.Background()
	c, cleanup, err := setup(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	server := towermcp.NewServer(c.workflow, c.store)
	return server.Run(ctx, &mcp.StdioTransport{})
}
