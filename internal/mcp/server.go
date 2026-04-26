// Package mcp exposes tower's worktree state and management as a
// Model Context Protocol server. Chat-side agents (Claude Code, Cursor,
// claude.ai) talk to this surface to list / add / remove worktrees and
// trigger syncs through the same workflow that the CLI and TUI use.
package mcp

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/itsHabib/tower/internal/store"
	"github.com/itsHabib/tower/internal/workflow"
)

// Version is the MCP server's reported implementation version.
const Version = "0.1.0"

// NewServer wires the workflow + store into an MCP server with every
// tool registered. Caller owns the transport (typically stdio).
func NewServer(wf *workflow.Service, s store.Store) *mcp.Server {
	h := &handlers{workflow: wf, store: s}
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "tower",
		Version: Version,
	}, nil)
	h.register(server)
	return server
}

type handlers struct {
	workflow *workflow.Service
	store    store.Store
}
