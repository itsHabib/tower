package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go sqlite driver registered as "sqlite"

	"github.com/itsHabib/tower/internal/domain"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS tasks (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    brief TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL,
    deps TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);

CREATE TABLE IF NOT EXISTS worktrees (
    task_id TEXT PRIMARY KEY,
    path TEXT NOT NULL,
    branch TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS pull_requests (
    task_id TEXT PRIMARY KEY,
    number INTEGER NOT NULL,
    url TEXT NOT NULL,
    state TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL,
    FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS reviews (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_number INTEGER NOT NULL,
    reviewer TEXT NOT NULL,
    state TEXT NOT NULL,
    body TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    UNIQUE(pr_number, reviewer, created_at)
);

CREATE INDEX IF NOT EXISTS idx_reviews_pr ON reviews(pr_number);

CREATE TABLE IF NOT EXISTS ci_checks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_number INTEGER NOT NULL,
    name TEXT NOT NULL,
    conclusion TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMP NOT NULL,
    UNIQUE(pr_number, name)
);

CREATE INDEX IF NOT EXISTS idx_ci_checks_pr ON ci_checks(pr_number);
`

type sqliteStore struct {
	db *sql.DB
}

// OpenSQLite opens or creates a SQLite-backed Store at the given path,
// applies the schema, and enables foreign-key cascades.
func OpenSQLite(ctx context.Context, path string) (Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &sqliteStore{db: db}, nil
}

func (s *sqliteStore) Close() error { return s.db.Close() }

func (s *sqliteStore) UpsertTask(ctx context.Context, t domain.Task) error {
	deps, err := json.Marshal(t.Deps)
	if err != nil {
		return fmt.Errorf("marshal deps: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO tasks (id, title, brief, path, deps, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    title = excluded.title,
    brief = excluded.brief,
    path = excluded.path,
    deps = excluded.deps,
    status = excluded.status,
    updated_at = excluded.updated_at
`, t.ID, t.Title, t.Brief, t.Path, string(deps), string(t.Status), t.CreatedAt, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert task: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetTask(ctx context.Context, id string) (*domain.Task, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, title, brief, path, deps, status, created_at, updated_at
FROM tasks WHERE id = ?`, id)
	var t domain.Task
	var depsJSON, status string
	if err := row.Scan(&t.ID, &t.Title, &t.Brief, &t.Path, &depsJSON, &status, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get task: %w", err)
	}
	if err := json.Unmarshal([]byte(depsJSON), &t.Deps); err != nil {
		return nil, fmt.Errorf("unmarshal deps: %w", err)
	}
	t.Status = domain.Status(status)
	return &t, nil
}

func (s *sqliteStore) ListTasks(ctx context.Context) ([]domain.Task, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, title, brief, path, deps, status, created_at, updated_at
FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Task
	for rows.Next() {
		var t domain.Task
		var depsJSON, status string
		if err := rows.Scan(&t.ID, &t.Title, &t.Brief, &t.Path, &depsJSON, &status, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		if err := json.Unmarshal([]byte(depsJSON), &t.Deps); err != nil {
			return nil, fmt.Errorf("unmarshal deps: %w", err)
		}
		t.Status = domain.Status(status)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *sqliteStore) DeleteTask(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetWorktree(ctx context.Context, wt domain.Worktree) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO worktrees (task_id, path, branch, created_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(task_id) DO UPDATE SET
    path = excluded.path,
    branch = excluded.branch
`, wt.TaskID, wt.Path, wt.Branch, wt.CreatedAt)
	if err != nil {
		return fmt.Errorf("set worktree: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetWorktree(ctx context.Context, taskID string) (*domain.Worktree, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT task_id, path, branch, created_at FROM worktrees WHERE task_id = ?`, taskID)
	var wt domain.Worktree
	if err := row.Scan(&wt.TaskID, &wt.Path, &wt.Branch, &wt.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get worktree: %w", err)
	}
	return &wt, nil
}

func (s *sqliteStore) DeleteWorktree(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM worktrees WHERE task_id = ?`, taskID)
	if err != nil {
		return fmt.Errorf("delete worktree: %w", err)
	}
	return nil
}

func (s *sqliteStore) SetPullRequest(ctx context.Context, pr domain.PullRequest) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO pull_requests (task_id, number, url, state, title, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(task_id) DO UPDATE SET
    number = excluded.number,
    url = excluded.url,
    state = excluded.state,
    title = excluded.title,
    updated_at = excluded.updated_at
`, pr.TaskID, pr.Number, pr.URL, string(pr.State), pr.Title, pr.CreatedAt, pr.UpdatedAt)
	if err != nil {
		return fmt.Errorf("set pull request: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetPullRequest(ctx context.Context, taskID string) (*domain.PullRequest, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT task_id, number, url, state, title, created_at, updated_at
FROM pull_requests WHERE task_id = ?`, taskID)
	var pr domain.PullRequest
	var state string
	if err := row.Scan(&pr.TaskID, &pr.Number, &pr.URL, &state, &pr.Title, &pr.CreatedAt, &pr.UpdatedAt); err != nil {
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
INSERT INTO reviews (pr_number, reviewer, state, body, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(pr_number, reviewer, created_at) DO UPDATE SET
    state = excluded.state,
    body = excluded.body
`, r.PRNumber, r.Reviewer, string(r.State), r.Body, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("upsert review: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListReviews(ctx context.Context, prNumber int) ([]domain.Review, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT pr_number, reviewer, state, body, created_at
FROM reviews WHERE pr_number = ? ORDER BY created_at ASC`, prNumber)
	if err != nil {
		return nil, fmt.Errorf("list reviews: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.Review
	for rows.Next() {
		var r domain.Review
		var state string
		if err := rows.Scan(&r.PRNumber, &r.Reviewer, &state, &r.Body, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan review: %w", err)
		}
		r.State = domain.ReviewState(state)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) UpsertCICheck(ctx context.Context, c domain.CICheck) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO ci_checks (pr_number, name, conclusion, url, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(pr_number, name) DO UPDATE SET
    conclusion = excluded.conclusion,
    url = excluded.url,
    updated_at = excluded.updated_at
`, c.PRNumber, c.Name, string(c.Conclusion), c.URL, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("upsert ci check: %w", err)
	}
	return nil
}

func (s *sqliteStore) ListCIChecks(ctx context.Context, prNumber int) ([]domain.CICheck, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT pr_number, name, conclusion, url, updated_at
FROM ci_checks WHERE pr_number = ? ORDER BY name ASC`, prNumber)
	if err != nil {
		return nil, fmt.Errorf("list ci checks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []domain.CICheck
	for rows.Next() {
		var c domain.CICheck
		var conclusion string
		if err := rows.Scan(&c.PRNumber, &c.Name, &conclusion, &c.URL, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan ci check: %w", err)
		}
		c.Conclusion = domain.CIConclusion(conclusion)
		out = append(out, c)
	}
	return out, rows.Err()
}
