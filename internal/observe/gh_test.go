package observe

import (
	"context"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
)

func TestPullRequestForBranch(t *testing.T) {
	body := `[
		{
			"number": 42,
			"url": "https://github.com/x/y/pull/42",
			"state": "OPEN",
			"title": "Add login flow",
			"createdAt": "2026-04-25T10:00:00Z",
			"updatedAt": "2026-04-25T11:30:00Z"
		}
	]`
	r := &fakeRunner{out: []byte(body)}
	gh := &GHObserver{Repo: "/repo", Runner: r}

	pr, err := gh.PullRequestForBranch(context.Background(), "tower/feat-login")
	if err != nil {
		t.Fatalf("pr: %v", err)
	}
	if pr == nil {
		t.Fatal("expected pr, got nil")
	}
	if pr.Number != 42 || pr.State != domain.PRStateOpen || pr.Title != "Add login flow" {
		t.Fatalf("unexpected pr: %+v", pr)
	}
	wantUpdated := time.Date(2026, 4, 25, 11, 30, 0, 0, time.UTC)
	if !pr.UpdatedAt.Equal(wantUpdated) {
		t.Fatalf("updated_at: want %v got %v", wantUpdated, pr.UpdatedAt)
	}
	if r.last.name != "gh" {
		t.Fatalf("runner: want gh, got %s", r.last.name)
	}
	hasHead := false
	for i, a := range r.last.args {
		if a == "--head" && i+1 < len(r.last.args) && r.last.args[i+1] == "tower/feat-login" {
			hasHead = true
		}
	}
	if !hasHead {
		t.Fatalf("expected --head tower/feat-login in args: %v", r.last.args)
	}
}

func TestPullRequestForBranchNoMatch(t *testing.T) {
	r := &fakeRunner{out: []byte(`[]`)}
	gh := &GHObserver{Repo: "/repo", Runner: r}
	pr, err := gh.PullRequestForBranch(context.Background(), "missing")
	if err != nil {
		t.Fatalf("pr: %v", err)
	}
	if pr != nil {
		t.Fatalf("expected nil pr, got %+v", pr)
	}
}

func TestParseReviews(t *testing.T) {
	body := `{
		"reviews": [
			{"author": {"login": "claude"}, "state": "COMMENTED", "body": "nit", "submittedAt": "2026-04-25T10:00:00Z"},
			{"author": {"login": "copilot"}, "state": "APPROVED", "body": "lgtm", "submittedAt": "2026-04-25T10:05:00Z"},
			{"author": {"login": "codex"}, "state": "CHANGES_REQUESTED", "body": "tests missing", "submittedAt": "2026-04-25T10:10:00Z"}
		]
	}`
	got, err := parseReviews([]byte(body), 7)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 reviews, got %d", len(got))
	}
	expected := []struct {
		reviewer string
		state    domain.ReviewState
	}{
		{"claude", domain.ReviewCommented},
		{"copilot", domain.ReviewApproved},
		{"codex", domain.ReviewChangesRequested},
	}
	for i, want := range expected {
		if got[i].Reviewer != want.reviewer || got[i].State != want.state || got[i].PRNumber != 7 {
			t.Errorf("review[%d]: want %+v got %+v", i, want, got[i])
		}
	}
}

func TestParseChecks(t *testing.T) {
	body := `{
		"statusCheckRollup": [
			{"name": "build", "status": "COMPLETED", "conclusion": "SUCCESS", "detailsUrl": "https://ci/build", "completedAt": "2026-04-25T10:00:00Z"},
			{"name": "test", "status": "COMPLETED", "conclusion": "FAILURE", "detailsUrl": "https://ci/test", "completedAt": "2026-04-25T10:01:00Z"},
			{"name": "lint", "status": "IN_PROGRESS", "conclusion": "", "detailsUrl": "https://ci/lint", "startedAt": "2026-04-25T10:02:00Z"},
			{"name": "deploy", "status": "COMPLETED", "conclusion": "SKIPPED", "detailsUrl": "https://ci/deploy", "completedAt": "2026-04-25T10:03:00Z"},
			{"name": "smoke", "status": "COMPLETED", "conclusion": "CANCELLED", "detailsUrl": "https://ci/smoke", "completedAt": "2026-04-25T10:04:00Z"}
		]
	}`
	got, err := parseChecks([]byte(body), 9)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := map[string]domain.CIConclusion{
		"build":  domain.CISuccess,
		"test":   domain.CIFailure,
		"lint":   domain.CIPending,
		"deploy": domain.CISkipped,
		"smoke":  domain.CICancelled,
	}
	if len(got) != len(want) {
		t.Fatalf("want %d checks, got %d", len(want), len(got))
	}
	for _, c := range got {
		if c.PRNumber != 9 {
			t.Errorf("pr: want 9 got %d", c.PRNumber)
		}
		if c.Conclusion != want[c.Name] {
			t.Errorf("%s: want %s got %s", c.Name, want[c.Name], c.Conclusion)
		}
	}
}

func TestMapPRState(t *testing.T) {
	for in, want := range map[string]domain.PRState{
		"OPEN":   domain.PRStateOpen,
		"CLOSED": domain.PRStateClosed,
		"MERGED": domain.PRStateMerged,
	} {
		if got := mapPRState(in); got != want {
			t.Errorf("mapPRState(%q): want %s got %s", in, want, got)
		}
	}
}
