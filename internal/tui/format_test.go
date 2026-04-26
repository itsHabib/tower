package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
)

func TestSummarizeChecksEmpty(t *testing.T) {
	if got := SummarizeChecks(nil); got != "-" {
		t.Fatalf("want -, got %q", got)
	}
}

func TestSummarizeChecksMixed(t *testing.T) {
	checks := []domain.CICheck{
		{Conclusion: domain.CISuccess},
		{Conclusion: domain.CISuccess},
		{Conclusion: domain.CIFailure},
		{Conclusion: domain.CIPending},
	}
	got := SummarizeChecks(checks)
	if !strings.Contains(got, "2 ok") || !strings.Contains(got, "1 fail") || !strings.Contains(got, "1 pending") {
		t.Fatalf("missing parts: %q", got)
	}
}

func TestSummarizeReviewsLatestPerReviewer(t *testing.T) {
	reviews := []domain.Review{
		{Reviewer: "claude", State: domain.ReviewCommented},
		{Reviewer: "claude", State: domain.ReviewApproved},
		{Reviewer: "codex", State: domain.ReviewChangesRequested},
	}
	got := SummarizeReviews(reviews)
	if !strings.Contains(got, "claude ✓") {
		t.Fatalf("expected claude approved (latest wins): %q", got)
	}
	if !strings.Contains(got, "codex ✗") {
		t.Fatalf("expected codex changes-requested: %q", got)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"abc", 5, "abc"},
		{"abcdef", 5, "abcd…"},
		{"abc", 0, ""},
		{"abc", 1, "a"},
	}
	for _, c := range cases {
		if got := truncate(c.in, c.max); got != c.want {
			t.Errorf("truncate(%q, %d): want %q got %q", c.in, c.max, c.want, got)
		}
	}
}

func TestPadRight(t *testing.T) {
	if got := padRight("abc", 5); got != "abc  " {
		t.Errorf("padRight: %q", got)
	}
	if got := padRight("abcdef", 3); got != "abcdef" {
		t.Errorf("padRight no shrink: %q", got)
	}
}

func TestParseSortMode(t *testing.T) {
	cases := []struct {
		in   string
		want SortMode
		err  bool
	}{
		{"", SortAttention, false},
		{"attention", SortAttention, false},
		{"activity", SortActivity, false},
		{"name", SortName, false},
		{"weird", 0, true},
	}
	for _, c := range cases {
		got, err := ParseSortMode(c.in)
		if (err != nil) != c.err {
			t.Errorf("%q: err=%v want_err=%v", c.in, err, c.err)
		}
		if !c.err && got != c.want {
			t.Errorf("%q: got %v want %v", c.in, got, c.want)
		}
	}
}

func TestRowPriority(t *testing.T) {
	now := time.Now()
	wt := domain.Worktree{Branch: "tower/x", LastSeen: now}
	pr := &domain.PullRequest{Number: 1, State: domain.PRStateOpen}

	if RowPriority(wt, nil, nil, nil) != PriorityNone {
		t.Error("clean worktree should be PriorityNone")
	}

	wt.Dirty = true
	if RowPriority(wt, nil, nil, nil) != PriorityDirty {
		t.Error("dirty worktree should be PriorityDirty")
	}
	wt.Dirty = false

	if RowPriority(wt, pr, nil, nil) != PriorityReviewWaiting {
		t.Error("open PR with no reviews should be PriorityReviewWaiting")
	}

	reviews := []domain.Review{{Reviewer: "claude", State: domain.ReviewChangesRequested}}
	if RowPriority(wt, pr, reviews, nil) != PriorityChangesRequested {
		t.Error("changes-requested review should be PriorityChangesRequested")
	}

	checks := []domain.CICheck{{Name: "test", Conclusion: domain.CIFailure}}
	if RowPriority(wt, pr, reviews, checks) != PriorityCIFail {
		t.Error("CI failure should outrank everything")
	}
}
