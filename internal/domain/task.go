// Package domain holds the core types tower tracks: tasks, worktrees, pull
// requests, reviews, and CI checks. No behavior, no external dependencies.
package domain

import "time"

// Status is the lifecycle state of a task as tower understands it.
type Status string

// Task lifecycle values.
const (
	StatusDraft     Status = "draft"
	StatusActive    Status = "active"
	StatusBlocked   Status = "blocked"
	StatusMerged    Status = "merged"
	StatusAbandoned Status = "abandoned"
)

// Task is one unit of agentic work — usually one markdown brief in features/.
type Task struct {
	ID        string
	Title     string
	Brief     string
	Path      string
	Deps      []string
	Status    Status
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Worktree binds a task to a git worktree on disk.
type Worktree struct {
	TaskID    string
	Path      string
	Branch    string
	CreatedAt time.Time
}

// PRState mirrors the lifecycle of a GitHub pull request.
type PRState string

// PR lifecycle values.
const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
	PRStateMerged PRState = "merged"
)

// PullRequest is the state of the GitHub PR opened for a task's branch.
type PullRequest struct {
	TaskID    string
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
