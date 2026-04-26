package domain

import "time"

type Status string

const (
	StatusDraft     Status = "draft"
	StatusActive    Status = "active"
	StatusBlocked   Status = "blocked"
	StatusMerged    Status = "merged"
	StatusAbandoned Status = "abandoned"
)

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

type Worktree struct {
	TaskID    string
	Path      string
	Branch    string
	CreatedAt time.Time
}

type PRState string

const (
	PRStateOpen   PRState = "open"
	PRStateClosed PRState = "closed"
	PRStateMerged PRState = "merged"
)

type PullRequest struct {
	TaskID    string
	Number    int
	URL       string
	State     PRState
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ReviewState string

const (
	ReviewPending          ReviewState = "pending"
	ReviewCommented        ReviewState = "commented"
	ReviewApproved         ReviewState = "approved"
	ReviewChangesRequested ReviewState = "changes_requested"
)

type Review struct {
	PRNumber  int
	Reviewer  string
	State     ReviewState
	Body      string
	CreatedAt time.Time
}

type CIConclusion string

const (
	CISuccess   CIConclusion = "success"
	CIFailure   CIConclusion = "failure"
	CIPending   CIConclusion = "pending"
	CISkipped   CIConclusion = "skipped"
	CICancelled CIConclusion = "cancelled"
)

type CICheck struct {
	PRNumber   int
	Name       string
	Conclusion CIConclusion
	URL        string
	UpdatedAt  time.Time
}
