package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/tui"
)

func TestWriteJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, nil); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Fatalf("empty input should marshal as []; got %q", got)
	}
}

func TestWriteJSONNoPR(t *testing.T) {
	rows := []tui.RowData{{
		Worktree: domain.Worktree{Repo: "tower", Branch: "tower/foo", Path: "/p"},
	}}
	var buf bytes.Buffer
	if err := writeJSON(&buf, rows); err != nil {
		t.Fatal(err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("output not valid JSON: %v", err)
	}
	if len(parsed) != 1 {
		t.Fatalf("want 1 row, got %d", len(parsed))
	}
	row := parsed[0]
	if row["pr"] != nil {
		t.Errorf("expected pr to be JSON null when no PR tracked; got %v", row["pr"])
	}
	if rev, ok := row["reviews"].([]any); !ok || rev == nil {
		t.Errorf("expected reviews to be [] not null; got %v", row["reviews"])
	}
	if ch, ok := row["checks"].([]any); !ok || ch == nil {
		t.Errorf("expected checks to be [] not null; got %v", row["checks"])
	}
}

func TestWriteJSONFullRow(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	rows := []tui.RowData{{
		Worktree: domain.Worktree{
			Repo: "tower", Branch: "tower/foo", Path: "/p",
			HEAD: "abc", Title: "feat: x", Dirty: true,
			Ahead: 2, Behind: 1, LastCommit: now, CreatedAt: now, LastSeen: now,
		},
		PR: &domain.PullRequest{
			Repo: "tower", Branch: "tower/foo", Number: 42,
			URL: "https://github.com/o/r/pull/42", State: domain.PRStateOpen,
			Title: "feat: x", CreatedAt: now, UpdatedAt: now,
		},
		Reviews: []domain.Review{
			{Repo: "tower", PRNumber: 42, Reviewer: "claude", State: domain.ReviewApproved, CreatedAt: now},
		},
		Checks: []domain.CICheck{
			{Repo: "tower", PRNumber: 42, Name: "test", Conclusion: domain.CISuccess, UpdatedAt: now},
		},
	}}
	var buf bytes.Buffer
	if err := writeJSON(&buf, rows); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		`"branch": "tower/foo"`,
		`"dirty": true`,
		`"ahead": 2`,
		`"last_commit":`,
		`"number": 42`,
		`"state": "open"`,
		`"reviewer": "claude"`,
		`"conclusion": "success"`,
		`"pr_number": 42`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\nfull output:\n%s", want, out)
		}
	}
}

func TestRunLsGroupedTable(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if _, err := env.c.workflow.AddRepo(ctx, t.TempDir(), "myrepo"); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := env.c.store.UpsertWorktree(ctx, domain.Worktree{
		Repo: "myrepo", Branch: "tower/feat-x", Path: "/p/x",
		CreatedAt: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("seed worktree: %v", err)
	}

	var buf bytes.Buffer
	if err := runLs(ctx, env.c, lsOpts{noReconcile: true, sort: tui.SortAttention}, &buf); err != nil {
		t.Fatalf("runLs: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "myrepo") {
		t.Errorf("output missing repo header: %q", out)
	}
	if !strings.Contains(out, "tower/feat-x") {
		t.Errorf("output missing branch row: %q", out)
	}
}

func TestRunLsJSONEndToEnd(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()
	if _, err := env.c.workflow.AddRepo(ctx, t.TempDir(), "myrepo"); err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := env.c.store.UpsertWorktree(ctx, domain.Worktree{
		Repo: "myrepo", Branch: "tower/feat-x", Path: "/p/x",
		CreatedAt: now, LastSeen: now,
	}); err != nil {
		t.Fatalf("seed worktree: %v", err)
	}

	var buf bytes.Buffer
	if err := runLs(ctx, env.c, lsOpts{noReconcile: true, json: true, sort: tui.SortAttention}, &buf); err != nil {
		t.Fatalf("runLs: %v", err)
	}
	var parsed []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("not valid JSON: %v\noutput: %s", err, buf.String())
	}
	if len(parsed) != 1 {
		t.Fatalf("want 1 row, got %d", len(parsed))
	}
	wt, ok := parsed[0]["worktree"].(map[string]any)
	if !ok {
		t.Fatalf("worktree not an object: %v", parsed[0]["worktree"])
	}
	if wt["branch"] != "tower/feat-x" {
		t.Errorf("branch: %v", wt["branch"])
	}
}
