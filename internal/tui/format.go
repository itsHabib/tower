package tui

import (
	"fmt"
	"strings"

	"github.com/itsHabib/tower/internal/domain"
)

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
