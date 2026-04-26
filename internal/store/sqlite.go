package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go sqlite driver registered as "sqlite"

	"github.com/itsHabib/tower/internal/domain"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS repos (
    name TEXT PRIMARY KEY,
    path TEXT NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS worktrees (
    repo TEXT NOT NULL,
    branch TEXT NOT NULL,
    path TEXT NOT NULL,
    head TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    dirty INTEGER NOT NULL DEFAULT 0,
    ahead INTEGER NOT NULL DEFAULT 0,
    behind INTEGER NOT NULL DEFAULT 0,
    last_commit TIMESTAMP,
    created_at TIMESTAMP NOT NULL,
    last_seen TIMESTAMP NOT NULL,
    PRIMARY KEY (repo, branch),
    FOREIGN KEY (repo) REFERENCES repos(name) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_worktrees_repo ON worktrees(repo);

CREATE TABLE IF NOT EXISTS pull_requests (
    repo TEXT NOT NULL,
    branch TEXT NOT NULL,
    number INTEGER NOT NULL,
    url TEXT NOT NULL,
    state TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    PRIMARY KEY (repo, branch),
    FOREIGN KEY (repo, branch) REFERENCES worktrees(repo, branch) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    reviewer TEXT NOT NULL,
    state TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    UNIQUE(repo, pr_number, reviewer, created_at)
);

CREATE INDEX IF NOT EXISTS idx_reviews_pr ON reviews(repo, pr_number);

CREATE TABLE IF NOT EXISTS ci_checks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    repo TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    name TEXT NOT NULL,
    conclusion TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL,
    UNIQUE(repo, pr_number, name)
);

CREATE INDEX IF NOT EXISTS idx_ci_checks_pr ON ci_checks(repo, pr_number);
`

type sqliteStore struct {
	db *sql.DB
}

// OpenSQLite opens or creates a SQLite-backed Store at the given path,
// applies the schema, enables foreign-key cascades, and turns on WAL
// mode so concurrent readers (TUI + MCP server) don't trip SQLITE_BUSY.
func OpenSQLite(ctx context.Context, path string) (Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	for _, pragma := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }

func (s *sqliteStore) UpsertRepo(ctx context.Context, r domain.Repo) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO repos (name, path, created_at) VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET path = excluded.path
`, r.Name, r.Path, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("upsert repo: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetRepo(ctx context.Context, name string) (*domain.Repo, error) {
	row := s.db.QueryRowContext(ctx, `SELECT name, path, created_at FROM repos WHERE name = ?`, name)
	var r domain.Repo
	if err := row.Scan(&r.Name, &r.Path, &r.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get repo: %w", err)
	}
	return &r, nil
}

func (s *sqliteStore) ListRepos(ctx context.Context) ([]domain.Repo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, path, created_at FROM repos ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("list repos: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Repo
	for rows.Next() {
		var r domain.Repo
		if err := rows.Scan(&r.Name, &r.Path, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan repo: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteRepo(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM repos WHERE name = ?`, name)
	if err != nil {
		return fmt.Errorf("delete repo: %w", err)
	}
	return nil
}

func (s *sqliteStore) UpsertWorktree(ctx context.Context, w domain.Worktree) error {
	var lastCommit any
	if !w.LastCommit.IsZero() {
		lastCommit = w.LastCommit
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO worktrees (repo, branch, path, head, title, dirty, ahead, behind, last_commit, created_at, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(repo, branch) DO UPDATE SET
    path = excluded.path,
    head = excluded.head,
    title = excluded.title,
    dirty = excluded.dirty,
    ahead = excluded.ahead,
    behind = excluded.behind,
    last_commit = excluded.last_commit,
    last_seen = excluded.last_seen
`, w.Repo, w.Branch, w.Path, w.HEAD, w.Title, boolToInt(w.Dirty), w.Ahead, w.Behind, lastCommit, w.CreatedAt, w.LastSeen)
	if err != nil {
		return fmt.Errorf("upsert worktree: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetWorktree(ctx context.Context, repo, branch string) (*domain.Worktree, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT repo, branch, path, head, title, dirty, ahead, behind, last_commit, created_at, last_seen
FROM worktrees WHERE repo = ? AND branch = ?`, repo, branch)
	w, err := scanWorktree(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get worktree: %w", err)
	}
	return w, nil
}

func (s *sqliteStore) ListWorktrees(ctx context.Context) ([]domain.Worktree, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT repo, branch, path, head, title, dirty, ahead, behind, last_commit, created_at, last_seen
FROM worktrees ORDER BY repo ASC, last_seen DESC`)
	if err != nil {
		return nil, fmt.Errorf("list worktrees: %w", err)
	}
	return scanWorktrees(rows)
}

func (s *sqliteStore) ListWorktreesForRepo(ctx context.Context, repo string) ([]domain.Worktree, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT repo, branch, path, head, title, dirty, ahead, behind, last_commit, created_at, last_seen
FROM worktrees WHERE repo = ? ORDER BY last_seen DESC`, repo)
	if err != nil {
		return nil, fmt.Errorf("list worktrees for repo: %w", err)
	}
	return scanWorktrees(rows)
}

func scanWorktrees(rows *sql.Rows) ([]domain.Worktree, error) {
	defer func() { _ = rows.Close() }()
	var out []domain.Worktree
	for rows.Next() {
		w, err := scanWorktree(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan worktree: %w", err)
		}
		out = append(out, *w)
	}
	return out, rows.Err()
}

func scanWorktree(scan func(...any) error) (*domain.Worktree, error) {
	var w domain.Worktree
	var dirty int
	var lastCommit sql.NullTime
	if err := scan(&w.Repo, &w.Branch, &w.Path, &w.HEAD, &w.Title, &dirty, &w.Ahead, &w.Behind, &lastCommit, &w.CreatedAt, &w.LastSeen); err != nil {
		return nil, err
	}
	w.Dirty = dirty != 0
	if lastCommit.Valid {
		w.LastCommit = lastCommit.Time
	}
	return &w, nil
}

func (s *sqliteStore) DeleteWorktree(ctx context.Context, repo, branch string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM worktrees WHERE repo = ? AND branch = ?`, repo, branch)
	if err != nil {
		return fmt.Errorf("delete worktree: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetPullRequest(ctx context.Context, pr domain.PullRequest) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO pull_requests (repo, branch, number, url, state, title, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(repo, branch) DO UPDATE SET
    number = excluded.number,
    url = excluded.url,
    state = excluded.state,
    title = excluded.title,
    updated_at = excluded.updated_at
`, pr.Repo, pr.Branch, pr.Number, pr.URL, string(pr.State), pr.Title, pr.CreatedAt, pr.UpdatedAt)
	if err != nil {
		return fmt.Errorf("set pull request: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetPullRequest(ctx context.Context, repo, branch string) (*domain.PullRequest, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT repo, branch, number, url, state, title, created_at, updated_at
FROM pull_requests WHERE repo = ? AND branch = ?`, repo, branch)
	var pr domain.PullRequest
	var state string
	if err := row.Scan(&pr.Repo, &pr.Branch, &pr.Number, &pr.URL, &state, &pr.Title, &pr.CreatedAt, &pr.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get pull request: %w", err)
	}
	pr.State = domain.PRState(state)
	return &pr, nil
}

func (s *sqliteStore) UpsertReview(ctx context.Context, r domain.Review) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO reviews (repo, pr_number, reviewer, state, body, created_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(repo, pr_number, reviewer, created_at) DO UPDATE SET
    state = excluded.state,
    body = excluded.body
`, r.Repo, r.PRNumber, r.Reviewer, string(r.State), r.Body, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("upsert review: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListReviews(ctx context.Context, repo string, prNumber int) ([]domain.Review, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT repo, pr_number, reviewer, state, body, created_at
FROM reviews WHERE repo = ? AND pr_number = ? ORDER BY created_at ASC`, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Review
	for rows.Next() {
		var r domain.Review
		var state string
		if err := rows.Scan(&r.Repo, &r.PRNumber, &r.Reviewer, &state, &r.Body, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan review: %w", err)
		}
		r.State = domain.ReviewState(state)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpsertCICheck(ctx context.Context, c domain.CICheck) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO ci_checks (repo, pr_number, name, conclusion, url, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(repo, pr_number, name) DO UPDATE SET
    conclusion = excluded.conclusion,
    url = excluded.url,
    updated_at = excluded.updated_at
`, c.Repo, c.PRNumber, c.Name, string(c.Conclusion), c.URL, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert ci check: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListCIChecks(ctx context.Context, repo string, prNumber int) ([]domain.CICheck, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT repo, pr_number, name, conclusion, url, updated_at
FROM ci_checks WHERE repo = ? AND pr_number = ? ORDER BY name ASC`, repo, prNumber)
	if err != nil {
		return nil, fmt.Errorf("list ci checks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.CICheck
	for rows.Next() {
		var c domain.CICheck
		var conclusion string
		if err := rows.Scan(&c.Repo, &c.PRNumber, &c.Name, &conclusion, &c.URL, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan ci check: %w", err)
		}
		c.Conclusion = domain.CIConclusion(conclusion)
		out = append(out, c)
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
