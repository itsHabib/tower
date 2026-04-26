package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// SpawnClaudeWithNewWorktree opens a new terminal tab/window and runs
// `claude -w <name> [prompt]` from the repo path, so claude itself
// creates the worktree at <repoPath>/.claude/worktrees/<name>. If prompt
// is non-empty, it's passed as the initial user message so claude
// responds to it as the first turn of the interactive session.
func SpawnClaudeWithNewWorktree(ctx context.Context, repoPath, name, prompt string) error {
	if err := validateSpawnInputs(repoPath, name); err != nil {
		return err
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

// SpawnClaudeBackground starts `claude -w <name> -p <prompt>` detached
// from any terminal so it runs to completion in the background. Uses
// claude's headless mode (`-p`), so prompt is required — without one
// claude has nothing to do.
func SpawnClaudeBackground(ctx context.Context, repoPath, name, prompt string) error {
	if err := validateSpawnInputs(repoPath, name); err != nil {
		return err
	}
	if prompt == "" {
		return errors.New("background spawn requires a prompt; claude -p needs instructions")
	}
	switch runtime.GOOS {
	case "windows":
		// `cmd /c start /b` runs the program detached without a new window.
		// Build the inner command as one string so cmd's parser handles
		// the quoted prompt correctly.
		// CMD escaping: double internal quotes, then wrap. %q would use
		// Go-style backslash escapes which CMD doesn't understand.
		escaped := strings.ReplaceAll(prompt, `"`, `""`)
		inner := `start /b claude -w ` + name + ` -p "` + escaped + `"`
		cmd := exec.CommandContext(ctx, "cmd", "/c", inner)
		cmd.Dir = repoPath
		return cmd.Start()
	default:
		return fmt.Errorf("background claude spawn not yet supported on %s", runtime.GOOS)
	}
}

func validateSpawnInputs(repoPath, name string) error {
	if repoPath == "" {
		return errors.New("empty repo path")
	}
	if name == "" {
		return errors.New("empty worktree name")
	}
	if strings.ContainsAny(name, " \t\n\r;&|<>$\"'`\\/") {
		return errors.New("worktree name must not contain whitespace, slashes, or shell special chars")
	}
	return nil
}
