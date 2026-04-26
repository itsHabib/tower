package tui

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
)

// OpenInBrowser launches the user's default browser at url. Returns an
// error if the platform is unsupported or the launcher fails to start.
func OpenInBrowser(ctx context.Context, url string) error {
	if url == "" {
		return errors.New("empty url")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.CommandContext(ctx, "cmd", "/c", "start", "", url)
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	return cmd.Start()
}
