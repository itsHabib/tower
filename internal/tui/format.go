package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/itsHabib/tower/internal/domain"
)

// FormatAge renders a duration since t as a short human label
// ("just now", "5m ago", "2h ago", "3d ago"). Returns "" for zero time.
func FormatAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	}
}

// Priority captures how much a worktree wants the user's attention.
// Higher value = more urgent.
type Priority int

// Priority levels in increasing urgency.
const (
	PriorityNone Priority = iota
	PriorityDirty
	PriorityReviewWaiting
	PriorityChangesRequested
	PriorityCIFail
)

// RowPriority computes the highest-impact attention signal for a worktree.
func RowPriority(wt domain.Worktree, pr *domain.PullRequest, reviews []domain.Review, checks []domain.CICheck) Priority {
	if pr != nil {
		for _, c := range checks {
			if c.Conclusion == domain.CIFailure {
				return PriorityCIFail
			}
		}
		latest := latestPerReviewer(reviews)
		for _, st := range latest {
			if st == domain.ReviewChangesRequested {
				return PriorityChangesRequested
			}
		}
		if pr.State == domain.PRStateOpen && len(latest) == 0 {
			return PriorityReviewWaiting
		}
	}
	if wt.Dirty {
		return PriorityDirty
	}
	return PriorityNone
}

func latestPerReviewer(reviews []domain.Review) map[string]domain.ReviewState {
	out := map[string]domain.ReviewState{}
	for _, r := range reviews {
		out[r.Reviewer] = r.State
	}
	return out
}

// SummarizeChecks renders a one-line summary of CI check outcomes.
func SummarizeChecks(checks []domain.CICheck) string {
	if len(checks) == 0 {
		return "-"
	}
	counts := map[domain.CIConclusion]int{}
	for _, c := range checks {
		counts[c.Conclusion]++
	}
	order := []struct {
		conc  domain.CIConclusion
		label string
	}{
		{domain.CISuccess, "ok"},
		{domain.CIFailure, "fail"},
		{domain.CIPending, "pending"},
		{domain.CISkipped, "skip"},
		{domain.CICanceled, "cancel"},
	}
	parts := make([]string, 0, len(order))
	for _, o := range order {
		if counts[o.conc] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[o.conc], o.label))
		}
	}
	return strings.Join(parts, " · ")
}

// SummarizeReviews renders a per-reviewer status pill string.
func SummarizeReviews(reviews []domain.Review) string {
	if len(reviews) == 0 {
		return "-"
	}
	latest := map[string]domain.ReviewState{}
	for _, r := range reviews {
		latest[r.Reviewer] = r.State
	}
	parts := make([]string, 0, len(latest))
	for reviewer, state := range latest {
		parts = append(parts, fmt.Sprintf("%s %s", reviewer, reviewSymbol(state)))
	}
	return strings.Join(parts, " ")
}

func reviewSymbol(s domain.ReviewState) string {
	switch s {
	case domain.ReviewApproved:
		return "✓"
	case domain.ReviewChangesRequested:
		return "✗"
	case domain.ReviewCommented:
		return "·"
	case domain.ReviewPending:
		return "?"
	}
	return string(s)
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func padRight(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}
