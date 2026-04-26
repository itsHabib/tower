package domain

// WorktreeView is a worktree composed with the PR, reviews, and CI
// state attached to it — the shape returned by tower's external read
// APIs (`tower ls --json` and the MCP server). PR is nullable because
// "no PR tracked" is a meaningful state. Reviews and Checks are
// always non-nil so consumers can iterate without null checks.
type WorktreeView struct {
	Worktree Worktree     `json:"worktree"`
	PR       *PullRequest `json:"pr"`
	Reviews  []Review     `json:"reviews"`
	Checks   []CICheck    `json:"checks"`
}
