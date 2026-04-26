package observe

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/itsHabib/tower/internal/domain"
)

type GHObserver struct {
	Repo   string
	Runner Runner
}

func NewGH(repoDir string) *GHObserver {
	return &GHObserver{Repo: repoDir, Runner: ExecRunner{}}
}

type ghPullRequest struct {
	Number    int       `json:"number"`
	URL       string    `json:"url"`
	State     string    `json:"state"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (g *GHObserver) PullRequestForBranch(ctx context.Context, branch string) (*domain.PullRequest, error) {
	out, err := g.Runner.Run(ctx, g.Repo,
		"gh", "pr", "list",
		"--head", branch,
		"--state", "all",
		"--json", "number,url,state,title,createdAt,updatedAt",
		"--limit", "1",
	)
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	prs, err := parsePullRequests(out)
	if err != nil {
		return nil, err
	}
	if len(prs) == 0 {
		return nil, nil
	}
	first := prs[0]
	return &domain.PullRequest{
		Number:    first.Number,
		URL:       first.URL,
		State:     mapPRState(first.State),
		Title:     first.Title,
		CreatedAt: first.CreatedAt,
		UpdatedAt: first.UpdatedAt,
	}, nil
}

func parsePullRequests(data []byte) ([]ghPullRequest, error) {
	var prs []ghPullRequest
	if err := json.Unmarshal(data, &prs); err != nil {
		return nil, fmt.Errorf("parse pr json: %w", err)
	}
	return prs, nil
}

func mapPRState(s string) domain.PRState {
	switch s {
	case "OPEN":
		return domain.PRStateOpen
	case "CLOSED":
		return domain.PRStateClosed
	case "MERGED":
		return domain.PRStateMerged
	default:
		return domain.PRState(s)
	}
}

type ghReviewsEnvelope struct {
	Reviews []ghReview `json:"reviews"`
}

type ghReview struct {
	Author      ghAuthor  `json:"author"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	SubmittedAt time.Time `json:"submittedAt"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

func (g *GHObserver) Reviews(ctx context.Context, prNumber int) ([]domain.Review, error) {
	out, err := g.Runner.Run(ctx, g.Repo,
		"gh", "pr", "view", strconv.Itoa(prNumber),
		"--json", "reviews",
	)
	if err != nil {
		return nil, fmt.Errorf("gh pr view reviews: %w", err)
	}
	return parseReviews(out, prNumber)
}

func parseReviews(data []byte, prNumber int) ([]domain.Review, error) {
	var env ghReviewsEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse reviews json: %w", err)
	}
	out := make([]domain.Review, 0, len(env.Reviews))
	for _, r := range env.Reviews {
		out = append(out, domain.Review{
			PRNumber:  prNumber,
			Reviewer:  r.Author.Login,
			State:     mapReviewState(r.State),
			Body:      r.Body,
			CreatedAt: r.SubmittedAt,
		})
	}
	return out, nil
}

func mapReviewState(s string) domain.ReviewState {
	switch s {
	case "APPROVED":
		return domain.ReviewApproved
	case "CHANGES_REQUESTED":
		return domain.ReviewChangesRequested
	case "COMMENTED":
		return domain.ReviewCommented
	case "PENDING":
		return domain.ReviewPending
	default:
		return domain.ReviewState(s)
	}
}

type ghChecksEnvelope struct {
	StatusCheckRollup []ghCheck `json:"statusCheckRollup"`
}

type ghCheck struct {
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	DetailsURL  string    `json:"detailsUrl"`
	CompletedAt time.Time `json:"completedAt"`
	StartedAt   time.Time `json:"startedAt"`
}

func (g *GHObserver) Checks(ctx context.Context, prNumber int) ([]domain.CICheck, error) {
	out, err := g.Runner.Run(ctx, g.Repo,
		"gh", "pr", "view", strconv.Itoa(prNumber),
		"--json", "statusCheckRollup",
	)
	if err != nil {
		return nil, fmt.Errorf("gh pr view checks: %w", err)
	}
	return parseChecks(out, prNumber)
}

func parseChecks(data []byte, prNumber int) ([]domain.CICheck, error) {
	var env ghChecksEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("parse checks json: %w", err)
	}
	out := make([]domain.CICheck, 0, len(env.StatusCheckRollup))
	for _, c := range env.StatusCheckRollup {
		updated := c.CompletedAt
		if updated.IsZero() {
			updated = c.StartedAt
		}
		out = append(out, domain.CICheck{
			PRNumber:   prNumber,
			Name:       c.Name,
			Conclusion: mapCheckConclusion(c.Status, c.Conclusion),
			URL:        c.DetailsURL,
			UpdatedAt:  updated,
		})
	}
	return out, nil
}

func mapCheckConclusion(status, conclusion string) domain.CIConclusion {
	if status != "" && status != "COMPLETED" {
		return domain.CIPending
	}
	switch conclusion {
	case "SUCCESS":
		return domain.CISuccess
	case "FAILURE", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
		return domain.CIFailure
	case "SKIPPED", "NEUTRAL":
		return domain.CISkipped
	case "CANCELLED":
		return domain.CICancelled
	case "":
		return domain.CIPending
	default:
		return domain.CIConclusion(conclusion)
	}
}
