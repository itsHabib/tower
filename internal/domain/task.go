// Package domain holds the core types tower tracks: repos, worktrees,
// and the pull-request, review, and CI state attached to each branch.
package domain

import "time"

// Repo is a registered git repository tower watches.
type Repo struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

// Worktree is one git worktree as tower sees it. Identity is (Repo, Branch).
type Worktree struct {
	Repo       string    `json:"repo"`
	Branch     string    `json:"branch"`
	Path       string    `json:"path"`
	HEAD       string    `json:"head"`
	Title      string    `json:"title"`
	Dirty      bool      `json:"dirty"`
	Ahead      int       `json:"ahead"`
	Behind     int       `json:"behind"`
	LastCommit time.Time `json:"last_commit"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeen   time.Time `json:"last_seen"`
}

// PRState mirrors the lifecycle of a GitHub pull request.
type PRState string

// PR lifecycle values.
const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
	PRStateMerged PRState = "merged"
)

// PullRequest is the latest known state of the PR opened for a branch in a repo.
type PullRequest struct {
	Repo      string    `json:"repo"`
	Branch    string    `json:"branch"`
	Number    int       `json:"number"`
	URL       string    `json:"url"`
	State     PRState   `json:"state"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
	Repo      string      `json:"repo"`
	PRNumber  int         `json:"pr_number"`
	Reviewer  string      `json:"reviewer"`
	State     ReviewState `json:"state"`
	Body      string      `json:"body"`
	CreatedAt time.Time   `json:"created_at"`
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
	Repo       string       `json:"repo"`
	PRNumber   int          `json:"pr_number"`
	Name       string       `json:"name"`
	Conclusion CIConclusion `json:"conclusion"`
	URL        string       `json:"url"`
	UpdatedAt  time.Time    `json:"updated_at"`
}
