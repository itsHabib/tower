// Package domain holds the core types tower tracks: worktrees and the
// pull-request, review, and CI state they accumulate.
package domain

import "time"

// Worktree is one git worktree as tower sees it. Identity is the branch.
type Worktree struct {
	Branch     string
	Path       string
	HEAD       string
	Title      string
	Dirty      bool
	Ahead      int
	Behind     int
	LastCommit time.Time
	CreatedAt  time.Time
	LastSeen   time.Time
}

// PRState mirrors the lifecycle of a GitHub pull request.
type PRState string

// PR lifecycle values.
const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
	PRStateMerged PRState = "merged"
)

// PullRequest is the latest known state of the PR opened for a branch.
type PullRequest struct {
	Branch    string
	Number    int
	URL       string
	State     PRState
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ReviewState is the disposition of a single review on a pull request.
type ReviewState string

// Review disposition values.
const (
	ReviewPending          ReviewState = "pending"
	ReviewCommented        ReviewState = "commented"
	ReviewApproved         ReviewState = "approved"
	ReviewChangesRequested ReviewState = "changes_requested"
)

// Review is one review left by a human or agent on a pull request.
type Review struct {
	PRNumber  int
	Reviewer  string
	State     ReviewState
	Body      string
	CreatedAt time.Time
}

// CIConclusion is the outcome of a single CI check on a pull request.
type CIConclusion string

// CI conclusion values.
const (
	CISuccess  CIConclusion = "success"
	CIFailure  CIConclusion = "failure"
	CIPending  CIConclusion = "pending"
	CISkipped  CIConclusion = "skipped"
	CICanceled CIConclusion = "canceled"
)

// CICheck is the latest known state of a single CI check on a pull request.
type CICheck struct {
	PRNumber   int
	Name       string
	Conclusion CIConclusion
	URL        string
	UpdatedAt  time.Time
}
