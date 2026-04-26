// Package tui renders the bubbletea board view that ties together the
// store and the workflow service for interactive use.
package tui

import (
	"context"
	"fmt"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/store"
	"github.com/itsHabib/tower/internal/workflow"
)

// AutoRefreshInterval is how often the TUI re-syncs in the background.
const AutoRefreshInterval = 60 * time.Second

// Run starts the TUI bound to the given workflow service and store.
// Blocks until the user quits.
func Run(ctx context.Context, wf *workflow.Service, s store.Store) error {
	m := newModel(ctx, wf, s)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	if m.openOnExit != "" {
		fmt.Println(m.openOnExit)
	}
	return nil
}

// ViewMode toggles between grouped-by-repo and flat list.
type ViewMode int

// View modes.
const (
	ViewGrouped ViewMode = iota
	ViewFlat
)

// Model is the bubbletea model for the worktree board view.
type Model struct {
	ctx        context.Context
	workflow   *workflow.Service
	store      store.Store
	rows       []worktreeRow
	cursor     int
	mode       ViewMode
	syncing    bool
	lastSync   time.Time
	err        error
	noRepos    bool
	width      int
	height     int
	openOnExit string
}

type worktreeRow struct {
	wt       domain.Worktree
	pr       *domain.PullRequest
	reviews  []domain.Review
	checks   []domain.CICheck
	priority Priority
}

func newModel(ctx context.Context, wf *workflow.Service, s store.Store) *Model {
	return &Model{ctx: ctx, workflow: wf, store: s}
}

// Init paints whatever is cached in the store immediately, then kicks
// off a background sync so PR/CI data flows in without user input.
// Schedules a periodic refresh tick.
func (m *Model) Init() tea.Cmd {
	m.syncing = true
	return tea.Batch(
		loadCmd(m.ctx, m.workflow, m.store),
		syncCmd(m.ctx, m.workflow),
		tickCmd(AutoRefreshInterval),
	)
}

type tickMsg time.Time

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type loadedMsg struct {
	rows    []worktreeRow
	noRepos bool
	err     error
}

type syncedMsg struct{ err error }

type reconciledMsg struct{ err error }

func reconcileCmd(ctx context.Context, wf *workflow.Service) tea.Cmd {
	return func() tea.Msg {
		return reconciledMsg{err: wf.Reconcile(ctx)}
	}
}

func loadCmd(ctx context.Context, wf *workflow.Service, s store.Store) tea.Cmd {
	return func() tea.Msg {
		repos, err := wf.ListRepos(ctx)
		if err != nil {
			return loadedMsg{err: err}
		}
		worktrees, err := s.ListWorktrees(ctx)
		if err != nil {
			return loadedMsg{err: err}
		}
		rows := make([]worktreeRow, 0, len(worktrees))
		for _, wt := range worktrees {
			r, err := loadRow(ctx, s, wt)
			if err != nil {
				return loadedMsg{err: err}
			}
			r.priority = RowPriority(r.wt, r.pr, r.reviews, r.checks)
			rows = append(rows, r)
		}
		SortRows(rows, SortAttention)
		return loadedMsg{rows: rows, noRepos: len(repos) == 0}
	}
}

// SortRows orders rows in place by the given mode. Group order in the
// grouped view is independent — this only affects within-group order
// (and the entire ordering in flat view).
func SortRows(rows []worktreeRow, mode SortMode) {
	switch mode {
	case SortAttention:
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].priority != rows[j].priority {
				return rows[i].priority > rows[j].priority
			}
			return rows[i].wt.LastSeen.After(rows[j].wt.LastSeen)
		})
	case SortActivity:
		sort.SliceStable(rows, func(i, j int) bool {
			return rows[i].wt.LastSeen.After(rows[j].wt.LastSeen)
		})
	case SortName:
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].wt.Repo != rows[j].wt.Repo {
				return rows[i].wt.Repo < rows[j].wt.Repo
			}
			return rows[i].wt.Branch < rows[j].wt.Branch
		})
	}
}

func loadRow(ctx context.Context, s store.Store, wt domain.Worktree) (worktreeRow, error) {
	r := worktreeRow{wt: wt}
	pr, err := s.GetPullRequest(ctx, wt.Repo, wt.Branch)
	if err != nil {
		return r, fmt.Errorf("pr %s/%s: %w", wt.Repo, wt.Branch, err)
	}
	r.pr = pr
	if pr == nil {
		return r, nil
	}
	revs, err := s.ListReviews(ctx, wt.Repo, pr.Number)
	if err != nil {
		return r, fmt.Errorf("reviews %s/#%d: %w", wt.Repo, pr.Number, err)
	}
	r.reviews = revs
	checks, err := s.ListCIChecks(ctx, wt.Repo, pr.Number)
	if err != nil {
		return r, fmt.Errorf("checks %s/#%d: %w", wt.Repo, pr.Number, err)
	}
	r.checks = checks
	return r, nil
}

func syncCmd(ctx context.Context, wf *workflow.Service) tea.Cmd {
	return func() tea.Msg {
		_, err := wf.Sync(ctx)
		return syncedMsg{err: err}
	}
}

// Update routes incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case reconciledMsg:
		if msg.err != nil {
			m.err = msg.err
		}
		return m, nil
	case loadedMsg:
		m.rows = msg.rows
		m.noRepos = msg.noRepos
		m.err = msg.err
		if m.cursor >= len(m.rows) && len(m.rows) > 0 {
			m.cursor = len(m.rows) - 1
		}
		if len(m.rows) == 0 {
			m.cursor = 0
		}
		return m, nil
	case syncedMsg:
		m.syncing = false
		m.lastSync = time.Now()
		if msg.err != nil {
			m.err = msg.err
		}
		return m, loadCmd(m.ctx, m.workflow, m.store)
	case tickMsg:
		next := tickCmd(AutoRefreshInterval)
		if m.syncing {
			return m, next
		}
		m.syncing = true
		return m, tea.Batch(syncCmd(m.ctx, m.workflow), next)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "s":
		if !m.syncing {
			m.syncing = true
			return m, syncCmd(m.ctx, m.workflow)
		}
	case "r":
		return m, tea.Sequence(reconcileCmd(m.ctx, m.workflow), loadCmd(m.ctx, m.workflow, m.store))
	case "g":
		if m.mode == ViewGrouped {
			m.mode = ViewFlat
		} else {
			m.mode = ViewGrouped
		}
	case "enter":
		if len(m.rows) > 0 {
			m.openOnExit = m.rows[m.cursor].wt.Path
			return m, tea.Quit
		}
	case "o":
		m.openCursorPR()
	}
	return m, nil
}

func (m *Model) openCursorPR() {
	if len(m.rows) == 0 {
		return
	}
	pr := m.rows[m.cursor].pr
	if pr == nil || pr.URL == "" {
		return
	}
	if err := OpenInBrowser(m.ctx, pr.URL); err != nil {
		m.err = err
	}
}
