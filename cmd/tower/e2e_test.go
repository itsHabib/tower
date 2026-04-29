//go:build integration

// True end-to-end tests that build the tower.exe binary and invoke it
// as a subprocess against a throwaway git repo. This exercises the
// real argv parsing, real workflow, real git shell-outs — the full
// stack from CLI down to disk.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildTower compiles the tower binary into the test temp dir and
// returns its path. Each test gets its own binary; that's cheap and
// isolates from any stale binary the developer left in the workspace.
func buildTower(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tower.exe")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/tower")
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// repoRoot walks up from the test file's package dir to find the
// module root (where go.mod lives), so `go build` runs in the right
// place regardless of where `go test` was invoked from.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, _ := os.Getwd()
	for d := wd; d != filepath.Dir(d); d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	t.Fatalf("go.mod not found upward from %s", wd)
	return ""
}

// freshRepo creates a git repo with one commit under t.TempDir() and
// returns its absolute path.
func freshRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not on PATH: %v", err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-qm", "initial"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

// run shells out to bin with args and an isolated state directory so
// each test has its own tower state.db. Returns combined stdout/stderr.
//
// We override every path Go's os.UserConfigDir reads on each platform:
//   - APPDATA          (Windows)
//   - XDG_CONFIG_HOME  (Linux when set)
//   - HOME             (Linux fallback, and macOS)
//
// Without all three, two CI tests can land their state.db in the same
// shared location and clobber each other.
func runCLI(t *testing.T, bin, stateDir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(),
		"APPDATA="+stateDir,
		"XDG_CONFIG_HOME="+stateDir,
		"HOME="+stateDir,
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestE2E_FullCLICycle(t *testing.T) {
	bin := buildTower(t)
	state := t.TempDir()
	repo := freshRepo(t)

	// repo add
	out, err := runCLI(t, bin, state, "repo", "add", repo)
	if err != nil {
		t.Fatalf("repo add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "registered:") {
		t.Fatalf("repo add output unexpected: %s", out)
	}

	// add worktree foo
	out, err = runCLI(t, bin, state, "add", "--repo", "repo", "foo")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "added: repo/tower/foo") {
		t.Fatalf("add output unexpected: %s", out)
	}

	// ls shows it
	out, err = runCLI(t, bin, state, "ls")
	if err != nil {
		t.Fatalf("ls: %v\n%s", err, out)
	}
	if !strings.Contains(out, "tower/foo") {
		t.Fatalf("ls missing tower/foo: %s", out)
	}

	// rm
	out, err = runCLI(t, bin, state, "rm", "--repo", "repo", "foo")
	if err != nil {
		t.Fatalf("rm: %v\n%s", err, out)
	}
	if !strings.Contains(out, "removed: repo/foo") {
		t.Fatalf("rm output unexpected: %s", out)
	}

	// branch should be gone (was clean / merged with main)
	branchOut, _ := exec.Command("git", "-C", repo, "branch").CombinedOutput()
	if strings.Contains(string(branchOut), "tower/foo") {
		t.Fatalf("branch tower/foo still present after rm: %s", branchOut)
	}

	// re-add same name should succeed (regression for "branch already exists")
	out, err = runCLI(t, bin, state, "add", "--repo", "repo", "foo")
	if err != nil {
		t.Fatalf("re-add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "added:") {
		t.Fatalf("re-add output: %s", out)
	}
}

func TestE2E_RmKeepsUnmergedBranch(t *testing.T) {
	bin := buildTower(t)
	state := t.TempDir()
	repo := freshRepo(t)

	if out, err := runCLI(t, bin, state, "repo", "add", repo); err != nil {
		t.Fatalf("repo add: %v\n%s", err, out)
	}
	if out, err := runCLI(t, bin, state, "add", "--repo", "repo", "wip"); err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}

	// Make an unmerged commit on tower/wip.
	wtPath := filepath.Join(repo, ".worktrees", "wip")
	for _, args := range [][]string{
		{"config", "user.email", "t@t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-qm", "wip-only"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = wtPath
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %v\n%s", err, out)
		}
	}

	// rm should fail with "branch kept (unmerged commits)" and exit non-zero.
	out, err := runCLI(t, bin, state, "rm", "--repo", "repo", "wip")
	if err == nil {
		t.Fatal("expected non-zero exit on unmerged branch")
	}
	if !strings.Contains(out, "branch kept") {
		t.Fatalf("expected unmerged-branch warning, got: %s", out)
	}

	// Branch should still be in git.
	branchOut, _ := exec.Command("git", "-C", repo, "branch").CombinedOutput()
	if !strings.Contains(string(branchOut), "tower/wip") {
		t.Fatalf("branch tower/wip should be preserved: %s", branchOut)
	}
}

func TestE2E_MCPServer_ListsTools(t *testing.T) {
	bin := buildTower(t)
	state := t.TempDir()
	repo := freshRepo(t)

	if out, err := runCLI(t, bin, state, "repo", "add", repo); err != nil {
		t.Fatalf("repo add: %v\n%s", err, out)
	}

	// Start the MCP server. It speaks JSON-RPC over stdio.
	cmd := exec.Command(bin, "mcp", "serve")
	cmd.Env = append(os.Environ(),
		"APPDATA="+state,
		"XDG_CONFIG_HOME="+state,
		"HOME="+state,
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	// Standard MCP handshake: initialize, then tools/list.
	send := func(payload string) {
		if _, err := io.WriteString(stdin, payload+"\n"); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	// Read a single line (JSON-RPC over stdio is newline-delimited),
	// with a deadline so a hung server doesn't hang the test forever.
	resp := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := stdout.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				if i := bytes.IndexByte(buf, '\n'); i >= 0 {
					resp <- string(buf[:i])
					return
				}
			}
			if err != nil {
				resp <- string(buf)
				return
			}
		}
	}()

	send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)
	select {
	case line := <-resp:
		if !strings.Contains(line, `"result"`) {
			t.Fatalf("initialize response unexpected: %s", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for initialize response")
	}

	// initialized notification
	send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	// Set up a fresh reader for tools/list.
	resp2 := make(chan string, 1)
	go func() {
		buf := make([]byte, 0, 8192)
		tmp := make([]byte, 1024)
		for {
			n, err := stdout.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				if i := bytes.IndexByte(buf, '\n'); i >= 0 {
					resp2 <- string(buf[:i])
					return
				}
			}
			if err != nil {
				resp2 <- string(buf)
				return
			}
		}
	}()

	send(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	var line string
	select {
	case line = <-resp2:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for tools/list")
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatalf("decode tools/list: %v\n%s", err, line)
	}
	result, _ := got["result"].(map[string]any)
	tools, _ := result["tools"].([]any)
	if len(tools) != 10 {
		t.Fatalf("want 10 tools, got %d. raw=%s", len(tools), line)
	}
	wantNames := []string{
		"list_worktrees", "get_worktree", "add_worktree", "remove_worktree",
		"sync", "reconcile", "list_repos", "register_repo", "unregister_repo",
		"prune_repos",
	}
	gotNames := map[string]bool{}
	for _, tIface := range tools {
		tMap, _ := tIface.(map[string]any)
		if name, ok := tMap["name"].(string); ok {
			gotNames[name] = true
		}
	}
	for _, name := range wantNames {
		if !gotNames[name] {
			t.Errorf("missing tool: %s", name)
		}
	}
}

func TestE2E_MCPServer_RegisterAndList(t *testing.T) {
	bin := buildTower(t)
	state := t.TempDir()
	repo := freshRepo(t)

	cmd := exec.Command(bin, "mcp", "serve")
	cmd.Env = append(os.Environ(),
		"APPDATA="+state,
		"XDG_CONFIG_HOME="+state,
		"HOME="+state,
	)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rpc := newJSONRPC(t, stdin, stdout)
	rpc.expect(ctx, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`, "result")
	rpc.notify(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	// Call register_repo via tools/call.
	registerCall := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"register_repo","arguments":{"path":"` + escapePath(repo) + `"}}}`
	resp := rpc.expect(ctx, registerCall, "result")
	if !strings.Contains(resp, `"name"`) {
		t.Fatalf("register_repo response missing name: %s", resp)
	}

	// Then list_repos should show it.
	listResp := rpc.expect(ctx, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_repos","arguments":{}}}`, "result")
	if !strings.Contains(listResp, "repo") {
		t.Fatalf("list_repos missing repo: %s", listResp)
	}
}

// escapePath turns a Windows path into a JSON-safe string (escaping
// backslashes). filepath.ToSlash isn't enough because some MCP tools
// validate the literal path, so we just escape.
func escapePath(p string) string {
	return strings.ReplaceAll(p, `\`, `\\`)
}

// jsonRPC is a tiny stdio JSON-RPC helper for the MCP tests.
type jsonRPC struct {
	t      *testing.T
	stdin  io.Writer
	stdout io.Reader
	buf    []byte
}

func newJSONRPC(t *testing.T, in io.Writer, out io.Reader) *jsonRPC {
	return &jsonRPC{t: t, stdin: in, stdout: out}
}

func (r *jsonRPC) notify(payload string) {
	if _, err := io.WriteString(r.stdin, payload+"\n"); err != nil {
		r.t.Fatalf("write: %v", err)
	}
}

func (r *jsonRPC) expect(ctx context.Context, payload, mustContain string) string {
	r.t.Helper()
	if _, err := io.WriteString(r.stdin, payload+"\n"); err != nil {
		r.t.Fatalf("write: %v", err)
	}
	line, err := readLine(ctx, r.stdout, &r.buf)
	if err != nil {
		r.t.Fatalf("read: %v", err)
	}
	if !strings.Contains(line, mustContain) {
		r.t.Fatalf("response missing %q: %s", mustContain, line)
	}
	return line
}

func readLine(ctx context.Context, r io.Reader, buf *[]byte) (string, error) {
	tmp := make([]byte, 1024)
	out := make(chan string, 1)
	errc := make(chan error, 1)
	go func() {
		for {
			n, err := r.Read(tmp)
			if n > 0 {
				*buf = append(*buf, tmp[:n]...)
				if i := bytes.IndexByte(*buf, '\n'); i >= 0 {
					line := string((*buf)[:i])
					*buf = (*buf)[i+1:]
					out <- line
					return
				}
			}
			if err != nil {
				errc <- err
				return
			}
		}
	}()
	select {
	case line := <-out:
		return line, nil
	case err := <-errc:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
