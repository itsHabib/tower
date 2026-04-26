// Package tui renders the bubbletea board view that ties together the
// store and the workflow service for interactive use.
package tui

import (
	"context"
	"errors"
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
	ctx         context.Context
	workflow    *workflow.Service
	store       store.Store
	rows        []worktreeRow
	cursor      int
	mode        ViewMode
	filter      string
	filtering   bool // true = keystrokes append to filter
	syncing     bool
	lastSync    time.Time
	err         error
	noRepos     bool
	width       int
	height      int
	openOnExit  string
	helpVisible bool
	input       inputMode
	inputBuf    string
	inputTarget worktreeRow
	stagedName  string      // carried between inputClaudeName and inputClaudePrompt
	spawnTarget SpawnTarget // chosen during inputClaudeSpawnMode
}

// inputMode tracks which prompt is currently collecting input from the
// user. Filter has its own field because it's a persistent display
// mode; the others are one-shot prompts that act on enter.
type inputMode int

// Input modes for one-shot prompts (a/d/c). inputNone = no prompt active.
const (
	inputNone            inputMode = iota
	inputAddName                   // typing a name for `a` (new tower-style worktree)
	inputClaudeSpawnMode           // picking [t]erminal / [b]ackground for `c`
	inputClaudeName                // typing a worktree name for `c`
	inputClaudePrompt              // typing optional initial prompt for `c`
	inputConfirmDelete             // showing y/N confirmation for `d`
)

// SpawnTarget controls where a claude session runs after `c` spawn.
type SpawnTarget int

// Spawn targets: terminal opens a new tab and chats interactively;
// background runs headless via `claude -p` and detaches from this
// process so the session keeps running if you quit tower.
const (
	SpawnTerminal SpawnTarget = iota
	SpawnBackground
)

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

type addedMsg struct {
	wt  *domain.Worktree
	err error
}

type removedMsg struct{ err error }

func addCmd(ctx context.Context, wf *workflow.Service, repoName, name string) tea.Cmd {
	return func() tea.Msg {
		wt, err := wf.Add(ctx, repoName, name)
		return addedMsg{wt: wt, err: err}
	}
}

func removeCmd(ctx context.Context, wf *workflow.Service, repoName, name string) tea.Cmd {
	return func() tea.Msg {
		return removedMsg{err: wf.Remove(ctx, repoName, name)}
	}
}

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
	case addedMsg:
		return m.afterMutation(msg.err)
	case removedMsg:
		return m.afterMutation(msg.err)
	}
	return m, nil
}

// afterMutation handles the common post-action flow for add/remove:
// surface the error if any, otherwise reload the rows so the board
// reflects the new state.
func (m *Model) afterMutation(err error) (tea.Model, tea.Cmd) {
	if err != nil {
		m.err = err
		return m, nil
	}
	m.err = nil
	return m, loadCmd(m.ctx, m.workflow, m.store)
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.helpVisible {
		switch msg.String() {
		case "?", "esc", "q":
			m.helpVisible = false
		}
		return m, nil
	}
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	if m.input != inputNone {
		return m.handleInputKey(msg)
	}
	if m.handleNavKey(msg) {
		return m, nil
	}
	return m.handleActionKey(msg)
}

func (m *Model) handleNavKey(msg tea.KeyMsg) bool {
	visible := m.visibleRows()
	switch msg.String() {
	case "j", "down":
		if m.cursor < len(visible)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "/":
		m.filtering = true
	case "esc":
		m.filter = ""
		m.cursor = 0
	default:
		return false
	}
	return true
}

func (m *Model) handleActionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
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
		visible := m.visibleRows()
		if len(visible) > 0 {
			m.openOnExit = visible[m.cursor].wt.Path
			return m, tea.Quit
		}
	case "o":
		m.openCursorPR()
	case "c":
		m.startClaudeSpawn()
	case "a":
		m.startAddName()
	case "d":
		m.startConfirmDelete()
	case "?":
		m.helpVisible = true
	}
	return m, nil
}

func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.filtering = false
		m.filter = ""
		m.cursor = 0
	case tea.KeyEnter:
		m.filtering = false
	case tea.KeyBackspace:
		if m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
			m.cursor = 0
		}
	case tea.KeyCtrlU:
		m.filter = ""
		m.cursor = 0
	case tea.KeyRunes, tea.KeySpace:
		m.filter += string(msg.Runes)
		m.cursor = 0
	default:
		// ignore other keys while filtering
	}
	return m, nil
}

func (m *Model) visibleRows() []worktreeRow {
	if m.filter == "" {
		return m.rows
	}
	f := lowerASCII(m.filter)
	out := make([]worktreeRow, 0, len(m.rows))
	for _, r := range m.rows {
		if matchesFilter(r, f) {
			out = append(out, r)
		}
	}
	return out
}

func (m *Model) openCursorPR() {
	visible := m.visibleRows()
	if len(visible) == 0 {
		return
	}
	pr := visible[m.cursor].pr
	if pr == nil || pr.URL == "" {
		return
	}
	if err := OpenInBrowser(m.ctx, pr.URL); err != nil {
		m.err = err
	}
}

// startAddName opens the prompt for `a`. Target repo comes from the
// cursor row; if the board is empty, errors out (no repo to infer).
func (m *Model) startAddName() {
	visible := m.visibleRows()
	if len(visible) == 0 {
		m.err = errors.New("no row to infer repo from; use `tower add` from the shell instead")
		return
	}
	m.inputTarget = visible[m.cursor]
	m.input = inputAddName
	m.inputBuf = ""
	m.err = nil
}

// startClaudeSpawn opens the prompt chain for `c`: pick spawn mode,
// type a worktree name, type an optional initial prompt, then spawn.
// Cursor row's repo is the parent for `claude -w`.
func (m *Model) startClaudeSpawn() {
	visible := m.visibleRows()
	if len(visible) == 0 {
		m.err = errors.New("no row to infer repo from")
		return
	}
	m.inputTarget = visible[m.cursor]
	m.input = inputClaudeSpawnMode
	m.inputBuf = ""
	m.stagedName = ""
	m.err = nil
}

// startConfirmDelete opens the y/N confirmation for `d`. Refuses on
// the main worktree of a repo (path equals repo's registered path) so
// you can't accidentally tear down the primary checkout.
func (m *Model) startConfirmDelete() {
	visible := m.visibleRows()
	if len(visible) == 0 {
		return
	}
	row := visible[m.cursor]
	repo, err := m.store.GetRepo(m.ctx, row.wt.Repo)
	if err != nil {
		m.err = err
		return
	}
	if repo != nil && repo.Path == row.wt.Path {
		m.err = errors.New("refusing to remove main worktree of " + row.wt.Repo)
		return
	}
	m.inputTarget = row
	m.input = inputConfirmDelete
	m.err = nil
}

func (m *Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.input {
	case inputConfirmDelete:
		return m.handleConfirmKey(msg)
	case inputClaudeSpawnMode:
		return m.handleSpawnModeKey(msg)
	case inputNone, inputAddName, inputClaudeName, inputClaudePrompt:
		return m.handleTextInputKey(msg)
	}
	return m, nil
}

func (m *Model) handleSpawnModeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "t", "T":
		m.spawnTarget = SpawnTerminal
		m.input = inputClaudeName
	case "b", "B":
		m.spawnTarget = SpawnBackground
		m.input = inputClaudeName
	case "esc":
		m.input = inputNone
	}
	return m, nil
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		target := m.inputTarget
		m.input = inputNone
		return m, removeCmd(m.ctx, m.workflow, target.wt.Repo, target.wt.Branch)
	case "n", "N", "esc", "enter":
		m.input = inputNone
	}
	return m, nil
}

func (m *Model) handleTextInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.input = inputNone
		m.inputBuf = ""
		m.stagedName = ""
	case tea.KeyEnter:
		return m.executeTextInput()
	case tea.KeyBackspace:
		if m.inputBuf != "" {
			m.inputBuf = m.inputBuf[:len(m.inputBuf)-1]
		}
	case tea.KeyCtrlU:
		m.inputBuf = ""
	case tea.KeyRunes, tea.KeySpace:
		m.inputBuf += string(msg.Runes)
	default:
		// ignore other keys while in text input
	}
	return m, nil
}

func (m *Model) executeTextInput() (tea.Model, tea.Cmd) {
	buf := m.inputBuf
	mode := m.input
	target := m.inputTarget
	m.inputBuf = ""

	switch mode {
	case inputAddName:
		m.input = inputNone
		if buf == "" {
			m.err = errors.New("name required")
			return m, nil
		}
		return m, addCmd(m.ctx, m.workflow, target.wt.Repo, buf)
	case inputClaudeName:
		if buf == "" {
			m.input = inputNone
			m.err = errors.New("name required")
			return m, nil
		}
		// Move to the optional-prompt stage. Don't reset m.input yet.
		m.stagedName = buf
		m.input = inputClaudePrompt
		return m, nil
	case inputClaudePrompt:
		if m.spawnTarget == SpawnBackground && buf == "" {
			// Background mode needs a prompt — claude -p has nothing to do
			// without one. Stay in the prompt stage so the user can retype.
			m.err = errors.New("background spawn requires a prompt")
			return m, nil
		}
		m.input = inputNone
		repo, err := m.store.GetRepo(m.ctx, target.wt.Repo)
		if err != nil {
			m.err = err
			return m, nil
		}
		if repo == nil {
			m.err = errors.New("repo not found: " + target.wt.Repo)
			return m, nil
		}
		var spawnErr error
		switch m.spawnTarget {
		case SpawnTerminal:
			spawnErr = SpawnClaudeWithNewWorktree(m.ctx, repo.Path, m.stagedName, buf)
		case SpawnBackground:
			spawnErr = SpawnClaudeBackground(m.ctx, repo.Path, m.stagedName, buf)
		}
		if spawnErr != nil {
			m.err = spawnErr
		}
		m.stagedName = ""
	case inputNone, inputClaudeSpawnMode, inputConfirmDelete:
		// not reachable here (filtered by handleInputKey).
	}
	return m, nil
}
