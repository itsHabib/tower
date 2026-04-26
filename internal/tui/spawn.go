package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// SpawnClaudeInTerminal opens a new terminal tab/window with `claude`
// running in the given worktree path. Cross-platform via runtime.GOOS:
// Windows uses Windows Terminal (`wt`); other platforms not yet wired up.
func SpawnClaudeInTerminal(ctx context.Context, path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	switch runtime.GOOS {
	case "windows":
		// `wt nt -d <path> claude` opens a new tab in Windows Terminal,
		// sets the working dir, and launches claude in it.
		cmd := exec.CommandContext(ctx, "wt", "nt", "-d", path, "claude")
		return cmd.Start()
	default:
		return fmt.Errorf("spawn claude in terminal not yet supported on %s", runtime.GOOS)
	}
}

// SpawnClaudeWithNewWorktree opens a new terminal tab/window and runs
// `claude -w <name> [prompt]` from the repo path, so claude itself
// creates the worktree at <repoPath>/.claude/worktrees/<name>. If prompt
// is non-empty, it's passed as the initial user message so claude
// responds to it as the first turn of the interactive session.
func SpawnClaudeWithNewWorktree(ctx context.Context, repoPath, name, prompt string) error {
	if repoPath == "" {
		return errors.New("empty repo path")
	}
	if name == "" {
		return errors.New("empty worktree name")
	}
	if strings.ContainsAny(name, " \t\n\r;&|<>$\"'`\\/") {
		return errors.New("worktree name must not contain whitespace, slashes, or shell special chars")
	}
	cmdLine := "claude -w " + name
	if prompt != "" {
		// CMD-style escape: double internal double-quotes, wrap whole thing.
		cmdLine += ` "` + strings.ReplaceAll(prompt, `"`, `""`) + `"`
	}
	switch runtime.GOOS {
	case "windows":
		// Pass the command as a single argv element so wt treats the
		// trailing `-w` as claude's flag, not its own window selector.
		cmd := exec.CommandContext(ctx, "wt", "nt", "-d", repoPath, cmdLine)
		return cmd.Start()
	default:
		return fmt.Errorf("spawn claude with worktree not yet supported on %s", runtime.GOOS)
	}
}
