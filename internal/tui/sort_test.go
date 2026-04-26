package tui

import (
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
)

func mkRow(repo, branch string, p Priority, lastSeen time.Time) worktreeRow {
	return worktreeRow{
		wt:       domain.Worktree{Repo: repo, Branch: branch, LastSeen: lastSeen},
		priority: p,
	}
}

func TestSortAttentionTopsCIFail(t *testing.T) {
	now := time.Now().UTC()
	rows := []worktreeRow{
		mkRow("o", "clean", PriorityNone, now),
		mkRow("o", "dirty", PriorityDirty, now.Add(-1*time.Hour)),
		mkRow("o", "ci-fail", PriorityCIFail, now.Add(-2*time.Hour)),
		mkRow("o", "changes-req", PriorityChangesRequested, now.Add(-3*time.Hour)),
		mkRow("o", "review-wait", PriorityReviewWaiting, now.Add(-4*time.Hour)),
	}
	SortRows(rows, SortAttention)
	want := []string{"ci-fail", "changes-req", "review-wait", "dirty", "clean"}
	for i, branch := range want {
		if rows[i].wt.Branch != branch {
			t.Errorf("position %d: want %q got %q", i, branch, rows[i].wt.Branch)
		}
	}
}

func TestSortAttentionTiebreakOnLastSeen(t *testing.T) {
	now := time.Now().UTC()
	rows := []worktreeRow{
		mkRow("o", "old-fail", PriorityCIFail, now.Add(-2*time.Hour)),
		mkRow("o", "new-fail", PriorityCIFail, now),
	}
	SortRows(rows, SortAttention)
	if rows[0].wt.Branch != "new-fail" {
		t.Errorf("most recent should be first: %+v", rows)
	}
}

func TestSortActivity(t *testing.T) {
	now := time.Now().UTC()
	rows := []worktreeRow{
		mkRow("o", "old", PriorityCIFail, now.Add(-2*time.Hour)),
		mkRow("o", "new", PriorityNone, now),
	}
	SortRows(rows, SortActivity)
	if rows[0].wt.Branch != "new" {
		t.Errorf("activity should ignore priority: %+v", rows)
	}
}

func TestSortName(t *testing.T) {
	now := time.Now().UTC()
	rows := []worktreeRow{
		mkRow("roxiq", "z", PriorityCIFail, now),
		mkRow("orchestra", "b", PriorityNone, now),
		mkRow("orchestra", "a", PriorityNone, now),
	}
	SortRows(rows, SortName)
	want := []struct{ repo, branch string }{
		{"orchestra", "a"},
		{"orchestra", "b"},
		{"roxiq", "z"},
	}
	for i, w := range want {
		if rows[i].wt.Repo != w.repo || rows[i].wt.Branch != w.branch {
			t.Errorf("pos %d: want %s/%s got %s/%s", i, w.repo, w.branch, rows[i].wt.Repo, rows[i].wt.Branch)
		}
	}
}
