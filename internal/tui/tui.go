// Package tui renders the bubbletea board view that ties together the
// store and the workflow service for interactive use.
package tui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/store"
	"github.com/itsHabib/tower/internal/workflow"
)

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

// Model is the bubbletea model for the worktree board view.
type Model struct {
	ctx        context.Context
	workflow   *workflow.Service
	store      store.Store
	rows       []worktreeRow
	cursor     int
	syncing    bool
	lastSync   time.Time
	err        error
	width      int
	height     int
	openOnExit string
}

type worktreeRow struct {
	wt      domain.Worktree
	pr      *domain.PullRequest
	reviews []domain.Review
	checks  []domain.CICheck
}

func newModel(ctx context.Context, wf *workflow.Service, s store.Store) *Model {
	return &Model{ctx: ctx, workflow: wf, store: s}
}

// Init reconciles the worktree set from git, then loads rows for display.
func (m *Model) Init() tea.Cmd {
	return tea.Sequence(reconcileCmd(m.ctx, m.workflow), loadCmd(m.ctx, m.store))
}

type loadedMsg struct {
	rows []worktreeRow
	err  error
}

type syncedMsg struct{ err error }

type reconciledMsg struct{ err error }

func reconcileCmd(ctx context.Context, wf *workflow.Service) tea.Cmd {
	return func() tea.Msg {
		return reconciledMsg{err: wf.Reconcile(ctx)}
	}
}

func loadCmd(ctx context.Context, s store.Store) tea.Cmd {
	return func() tea.Msg {
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
			rows = append(rows, r)
		}
		return loadedMsg{rows: rows}
	}
}

func loadRow(ctx context.Context, s store.Store, wt domain.Worktree) (worktreeRow, error) {
	r := worktreeRow{wt: wt}
	pr, err := s.GetPullRequest(ctx, wt.Branch)
	if err != nil {
		return r, fmt.Errorf("pr %s: %w", wt.Branch, err)
	}
	r.pr = pr
	if pr == nil {
		return r, nil
	}
	revs, err := s.ListReviews(ctx, pr.Number)
	if err != nil {
		return r, fmt.Errorf("reviews %d: %w", pr.Number, err)
	}
	r.reviews = revs
	checks, err := s.ListCIChecks(ctx, pr.Number)
	if err != nil {
		return r, fmt.Errorf("checks %d: %w", pr.Number, err)
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
		return m, loadCmd(m.ctx, m.store)
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
		return m, tea.Sequence(reconcileCmd(m.ctx, m.workflow), loadCmd(m.ctx, m.store))
	case "enter":
		if len(m.rows) > 0 {
			m.openOnExit = m.rows[m.cursor].wt.Path
			return m, tea.Quit
		}
	}
	return m, nil
}
