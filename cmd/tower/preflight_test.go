package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightDirExisting(t *testing.T) {
	dir := t.TempDir()
	if err := preflightDir(dir, dir); err != nil {
		t.Fatalf("expected nil for existing dir: %v", err)
	}
}

func TestPreflightDirNotADirectory(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	if err := os.WriteFile(file, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	err := preflightDir(file, dir)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("want 'not a directory' error, got %v", err)
	}
}

func TestPreflightDirMissingShowsSuggestions(t *testing.T) {
	repo := t.TempDir()
	mustMkdir(t, filepath.Join(repo, "features"))
	mustWriteFile(t, filepath.Join(repo, "features", "a.md"), "# A")
	mustWriteFile(t, filepath.Join(repo, "features", "b.md"), "# B")
	mustMkdir(t, filepath.Join(repo, "specs"))
	mustWriteFile(t, filepath.Join(repo, "specs", "x.md"), "# X")
	mustMkdir(t, filepath.Join(repo, "binaries"))
	mustWriteFile(t, filepath.Join(repo, "binaries", "tool.exe"), "")
	mustMkdir(t, filepath.Join(repo, ".hidden"))
	mustWriteFile(t, filepath.Join(repo, ".hidden", "secret.md"), "x")

	err := preflightDir(filepath.Join(repo, "feature"), repo)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "does not exist") {
		t.Errorf("missing 'does not exist': %s", msg)
	}
	if !strings.Contains(msg, "features") {
		t.Errorf("expected features suggestion: %s", msg)
	}
	if !strings.Contains(msg, "specs") {
		t.Errorf("expected specs suggestion: %s", msg)
	}
	if strings.Contains(msg, "binaries") {
		t.Errorf("binaries dir has no .md, should not be suggested: %s", msg)
	}
	if strings.Contains(msg, ".hidden") {
		t.Errorf("hidden dirs should not be suggested: %s", msg)
	}
	// features (2 .md) should appear before specs (1 .md)
	fIdx := strings.Index(msg, "features")
	sIdx := strings.Index(msg, "specs")
	if fIdx < 0 || sIdx < 0 || fIdx > sIdx {
		t.Errorf("expected features sorted before specs: %s", msg)
	}
}

func TestPreflightDirMissingNoCandidates(t *testing.T) {
	repo := t.TempDir()
	err := preflightDir(filepath.Join(repo, "missing"), repo)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing 'does not exist': %v", err)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWriteFile(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}
