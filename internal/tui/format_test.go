package tui

import (
	"strings"
	"testing"

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
