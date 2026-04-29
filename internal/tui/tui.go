// Package tui renders the bubbletea board view that ties together the
// store and the workflow service for interactive use.
package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/itsHabib/tower/internal/domain"
	"github.com/itsHabib/tower/internal/store"
	"github.com/itsHabib/tower/internal/workflow"
)

// dbg logs to %TEMP%/tower-debug.log when TOWER_DEBUG=1 in the env.
// Always non-nil so callers don't need to nil-check.
var dbg = newDebugLogger()

func newDebugLogger() *log.Logger {
	if os.Getenv("TOWER_DEBUG") == "" {
		return log.New(io.Discard, "", 0)
	}
	path := filepath.Join(os.TempDir(), "tower-debug.log")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return log.New(io.Discard, "", 0)
	}
	l := log.New(f, "", log.LstdFlags|log.Lmicroseconds)
	l.Printf("=== tower TUI debug log opened at %s ===", path)
	return l
}

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
	repos       []domain.Repo
	cursor      int
	mode        ViewMode
	filter      string
	filtering   bool // true = keystrokes append to filter
	syncing     bool
	lastSync    time.Time
	err         error
	info        string // transient success message; cleared on next action
	noRepos     bool
	width       int
	height      int
	helpVisible bool
	detailRow   *worktreeRow // when non-nil, render the detail panel for this row
	selected    map[wtKey]bool
	input       inputMode
	inputBuf    string
	inputTarget worktreeRow
}

// wtKey identifies a worktree row across reloads — same (repo, branch)
// always refers to the same logical row, even if the slice index
// shifts on a refresh.
type wtKey struct{ repo, branch string }

func keyOf(r worktreeRow) wtKey { return wtKey{r.wt.Repo, r.wt.Branch} }

// inputMode tracks which prompt is currently collecting input from the
// user. Filter has its own field because it's a persistent display
// mode; the others are one-shot prompts that act on enter.
type inputMode int

// Input modes for one-shot prompts (a/r/d/D). inputNone = no prompt active.
const (
	inputNone               inputMode = iota
	inputAddName                      // typing a name for `a` (new tower-style worktree)
	inputAddRepoPath                  // typing a path for `r` (register a repo)
	inputConfirmDelete                // showing y/N confirmation for `d` (single)
	inputConfirmDeleteMulti           // showing y/N confirmation for `D` (selected)
)

type worktreeRow struct {
	wt       domain.Worktree
	pr       *domain.PullRequest
	reviews  []domain.Review
	checks   []domain.CICheck
	priority Priority
}

// repoRow is the one-row-per-repo summary rendered in grouped view.
// It aggregates over the worktrees the user can currently see (so it
// respects the active filter).
type repoRow struct {
	name       string
	path       string
	worktrees  int
	dirty      int
	openPRs    int
	failingCI  int
	lastCommit time.Time
	priority   Priority
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
	repos   []domain.Repo
	noRepos bool
	err     error
}

type syncedMsg struct{ err error }

type addedMsg struct {
	wt  *domain.Worktree
	err error
}

type removedMsg struct{ err error }

// removedManyMsg is the result of a bulk delete (`D`).
//   - removed: worktrees actually torn down (this includes rows where
//     the branch was kept due to unmerged commits — the *worktree* is
//     still gone, which is what the user asked for).
//   - branchKept: subset of `removed` where `git branch -d` was
//     refused. Surfaced separately so the user knows the branches are
//     still there if they want to force-delete them.
//   - failures: per-row errors that prevented even the worktree from
//     being removed. Keyed by "<repo>/<branch>".
type removedManyMsg struct {
	removed    int
	branchKept int
	failures   map[string]error
}

type repoAddedMsg struct {
	repo *domain.Repo
	err  error
}

func addCmd(ctx context.Context, wf *workflow.Service, repoName, name string) tea.Cmd {
	return func() tea.Msg {
		wt, err := wf.Add(ctx, repoName, name)
		return addedMsg{wt: wt, err: err}
	}
}

func removeCmd(ctx context.Context, wf *workflow.Service, repoName, name string, force bool) tea.Cmd {
	return func() tea.Msg {
		dbg.Printf("removeCmd: calling wf.Remove(repo=%q, name=%q, force=%v)", repoName, name, force)
		err := wf.Remove(ctx, repoName, name, force)
		if err != nil {
			dbg.Printf("removeCmd: wf.Remove returned err: %v", err)
		} else {
			dbg.Printf("removeCmd: wf.Remove returned nil (success)")
		}
		return removedMsg{err: err}
	}
}

// removeManyCmd tears down each row in `targets` sequentially, passing
// --force when the row was dirty (the user already saw the count and
// confirmed). Per-row failures don't abort the batch — they aggregate
// into removedManyMsg.failures so partial successes still surface.
//
// ErrBranchKeptUnmerged is treated as success-with-caveat: the
// worktree IS gone (the user's intent), only the branch ref remains.
// We bump both `removed` and `branchKept` so the summary can mention
// both numbers.
func removeManyCmd(ctx context.Context, wf *workflow.Service, targets []worktreeRow) tea.Cmd {
	return func() tea.Msg {
		out := removedManyMsg{failures: map[string]error{}}
		for _, t := range targets {
			err := wf.Remove(ctx, t.wt.Repo, t.wt.Branch, t.wt.Dirty)
			switch {
			case err == nil:
				out.removed++
			case errors.Is(err, workflow.ErrBranchKeptUnmerged):
				out.removed++
				out.branchKept++
			default:
				out.failures[t.wt.Repo+"/"+t.wt.Branch] = err
			}
		}
		return out
	}
}

func addRepoCmd(ctx context.Context, wf *workflow.Service, path string) tea.Cmd {
	return func() tea.Msg {
		r, err := wf.AddRepo(ctx, path, "")
		return repoAddedMsg{repo: r, err: err}
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
		return loadedMsg{rows: rows, repos: repos, noRepos: len(repos) == 0}
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

// Update routes incoming messages. Each case delegates to a named
// handler when there's any logic worth a name — keeps this dispatch
// thin so the message-flow stays readable at a glance.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case loadedMsg:
		return m.handleLoaded(msg)
	case syncedMsg:
		return m.handleSynced(msg)
	case tickMsg:
		return m.handleTick()
	case addedMsg:
		return m.afterMutation(msg.err)
	case removedMsg:
		dbg.Printf("Update: removedMsg arrived (err=%v)", msg.err)
		return m.afterMutation(msg.err)
	case removedManyMsg:
		return m.handleRemovedMany(msg)
	case repoAddedMsg:
		return m.handleRepoAdded(msg)
	}
	return m, nil
}

// handleLoaded folds a fresh row/repo snapshot into the model. It only
// overwrites m.err on a load-time failure: the common no-error case
// (every mutation and auto-refresh tick triggers a reload) must not
// clobber a freshly-set error from the action that caused the reload.
func (m *Model) handleLoaded(msg loadedMsg) (tea.Model, tea.Cmd) {
	m.rows = msg.rows
	m.repos = msg.repos
	m.noRepos = msg.noRepos
	if msg.err != nil {
		m.err = msg.err
	}
	n := m.visibleCount()
	switch {
	case n == 0:
		m.cursor = 0
	case m.cursor >= n:
		m.cursor = n - 1
	}
	return m, nil
}

// handleSynced clears the syncing indicator, records the timestamp,
// and triggers a reload so newly-discovered rows surface immediately.
func (m *Model) handleSynced(msg syncedMsg) (tea.Model, tea.Cmd) {
	m.syncing = false
	m.lastSync = time.Now()
	if msg.err != nil {
		m.err = msg.err
	}
	return m, loadCmd(m.ctx, m.workflow, m.store)
}

// handleTick re-arms the periodic refresh timer and starts a sync if
// none is in flight. Skipping the sync when one is already running
// avoids piling up overlapping shellouts.
func (m *Model) handleTick() (tea.Model, tea.Cmd) {
	next := tickCmd(AutoRefreshInterval)
	if m.syncing {
		return m, next
	}
	m.syncing = true
	return m, tea.Batch(syncCmd(m.ctx, m.workflow), next)
}

// handleRemovedMany processes a bulk-delete result. Selections drop for
// every row where the worktree-remove succeeded (clean OR branch-kept,
// since both leave the row gone from the board); only genuine failures
// keep their selection mark.
func (m *Model) handleRemovedMany(msg removedManyMsg) (tea.Model, tea.Cmd) {
	dbg.Printf("Update: removedManyMsg removed=%d branchKept=%d failures=%d", msg.removed, msg.branchKept, len(msg.failures))
	for k := range m.selected {
		if _, failed := msg.failures[k.repo+"/"+k.branch]; !failed {
			delete(m.selected, k)
		}
	}
	summary := fmt.Sprintf("removed %d worktrees", msg.removed)
	if msg.branchKept > 0 {
		summary += fmt.Sprintf(" (%d unmerged branch refs kept; `git branch -D` to discard)", msg.branchKept)
	}
	if len(msg.failures) == 0 {
		m.err = nil
		m.info = summary
	} else {
		var first error
		for _, e := range msg.failures {
			first = e
			break
		}
		m.err = fmt.Errorf("%s; %d failed: %s", summary, len(msg.failures), firstLine(first))
		m.info = ""
	}
	return m, loadCmd(m.ctx, m.workflow, m.store)
}

// handleRepoAdded shows the success/failure banner for `r` and kicks a
// sync now so the repo's worktrees show up immediately — without it,
// the user would press r and see nothing change until the next 60s
// auto-refresh fires, which reads as "nothing happened".
func (m *Model) handleRepoAdded(msg repoAddedMsg) (tea.Model, tea.Cmd) {
	m.info = ""
	if msg.err != nil {
		m.err = msg.err
		return m, loadCmd(m.ctx, m.workflow, m.store)
	}
	m.err = nil
	if msg.repo != nil {
		m.info = fmt.Sprintf("registered %s at %s — syncing to pick up worktrees…", msg.repo.Name, msg.repo.Path)
	}
	if !m.syncing {
		m.syncing = true
		return m, tea.Batch(syncCmd(m.ctx, m.workflow), loadCmd(m.ctx, m.workflow, m.store))
	}
	return m, loadCmd(m.ctx, m.workflow, m.store)
}

// afterMutation handles the common post-action flow for add/remove:
// always reload the rows (so partial successes like "worktree gone but
// branch kept" still update the board) and surface any error message.
func (m *Model) afterMutation(err error) (tea.Model, tea.Cmd) {
	m.err = err
	m.info = ""
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
	if m.detailRow != nil {
		return m.handleDetailKey(msg)
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

// handleDetailKey is the only handler that runs while the detail panel
// is up: esc / q / enter dismisses it; o opens the row's PR if any
// (mirrors the top-level `o`); ctrl+c still quits the program.
func (m *Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "enter":
		m.detailRow = nil
	case "o":
		if m.detailRow != nil && m.detailRow.pr != nil && m.detailRow.pr.URL != "" {
			if err := OpenInBrowser(m.ctx, m.detailRow.pr.URL); err != nil {
				m.err = err
			}
		}
	}
	return m, nil
}

func (m *Model) handleNavKey(msg tea.KeyMsg) bool {
	n := m.visibleCount()
	switch msg.String() {
	case "j", "down":
		if m.cursor < n-1 {
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
		m.selected = nil
		m.cursor = 0
	default:
		return false
	}
	return true
}

// visibleCount returns the number of rows currently rendered in the
// body — repos in grouped mode, worktrees in flat mode. Used to bound
// the cursor.
func (m *Model) visibleCount() int {
	if m.mode == ViewGrouped {
		return len(m.visibleRepos())
	}
	return len(m.visibleRows())
}

func (m *Model) handleActionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dbg.Printf("handleActionKey: %q (input=%d filter=%q filtering=%v helpVisible=%v)", msg.String(), m.input, m.filter, m.filtering, m.helpVisible)
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "s":
		return m.startSync()
	case "g":
		m.toggleMode()
	case " ":
		m.toggleSelectionAtCursor()
	case "D":
		return m.startMultiDeleteConfirm()
	case "enter":
		return m.handleEnter()
	case "o":
		m.openCursorPR()
	case "a":
		m.startAddName()
	case "r":
		m.startAddRepoPath()
	case "d":
		m.startConfirmDelete()
	case "?":
		m.helpVisible = true
	}
	return m, nil
}

// startSync kicks off a background sync if one isn't already in flight.
// Idempotent — pressing s repeatedly while syncing is a no-op.
func (m *Model) startSync() (tea.Model, tea.Cmd) {
	if m.syncing {
		return m, nil
	}
	m.syncing = true
	return m, syncCmd(m.ctx, m.workflow)
}

// toggleMode flips between grouped and flat. Selection only applies in
// flat view, so it gets cleared on the way to grouped — otherwise a
// later [D] would silently target rows the user can't see.
func (m *Model) toggleMode() {
	if m.mode == ViewGrouped {
		m.mode = ViewFlat
	} else {
		m.mode = ViewGrouped
		m.selected = nil
	}
	m.cursor = 0
}

// handleEnter does mode-specific things: in grouped view enter drills
// into the cursor repo (filter to its worktrees, switch to flat); in
// flat view it opens the detail panel for the cursor row.
func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.mode == ViewGrouped {
		repos := m.visibleRepos()
		if len(repos) > 0 && m.cursor < len(repos) {
			// Two registered repos can't share a name, so a repo-name
			// filter unambiguously narrows to that repo's worktrees.
			m.filter = repos[m.cursor].name
			m.mode = ViewFlat
			m.cursor = 0
		}
		return m, nil
	}
	visible := m.visibleRows()
	if len(visible) > 0 && m.cursor < len(visible) {
		row := visible[m.cursor]
		m.detailRow = &row
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

// visibleRepos aggregates the visible worktree rows into one row per
// repo, in attention-priority order (max priority across that repo's
// worktrees, most-recent activity as tiebreaker). Used to render the
// grouped view as a one-row-per-repo summary.
func (m *Model) visibleRepos() []repoRow {
	rows := m.visibleRows()
	byName := make(map[string]*repoRow, len(rows))
	order := make([]string, 0, len(rows))
	for _, r := range rows {
		rr, ok := byName[r.wt.Repo]
		if !ok {
			rr = &repoRow{name: r.wt.Repo, path: m.repoPath(r.wt.Repo)}
			byName[r.wt.Repo] = rr
			order = append(order, r.wt.Repo)
		}
		rr.worktrees++
		if r.wt.Dirty {
			rr.dirty++
		}
		if r.pr != nil && r.pr.State == domain.PRStateOpen {
			rr.openPRs++
		}
		for _, c := range r.checks {
			if c.Conclusion == domain.CIFailure {
				rr.failingCI++
				break
			}
		}
		if r.wt.LastCommit.After(rr.lastCommit) {
			rr.lastCommit = r.wt.LastCommit
		}
		if r.priority > rr.priority {
			rr.priority = r.priority
		}
	}
	out := make([]repoRow, 0, len(order))
	for _, n := range order {
		out = append(out, *byName[n])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].priority != out[j].priority {
			return out[i].priority > out[j].priority
		}
		return out[i].lastCommit.After(out[j].lastCommit)
	})
	return out
}

// repoPath returns the registered path for repo name, or "" if not
// found. The repos slice is small (handful of entries) so a linear
// scan is fine.
func (m *Model) repoPath(name string) string {
	for _, r := range m.repos {
		if r.Name == name {
			return r.Path
		}
	}
	return ""
}

// cursorPath returns the on-disk path for whatever the cursor points
// at: a worktree path in flat view, the repo's registered path in
// grouped view. Empty if there's nothing under the cursor.
func (m *Model) cursorPath() string {
	if m.mode == ViewGrouped {
		repos := m.visibleRepos()
		if m.cursor >= 0 && m.cursor < len(repos) {
			return repos[m.cursor].path
		}
		return ""
	}
	visible := m.visibleRows()
	if m.cursor >= 0 && m.cursor < len(visible) {
		return visible[m.cursor].wt.Path
	}
	return ""
}

func (m *Model) openCursorPR() {
	if m.mode == ViewGrouped {
		m.err = errors.New("o opens a PR — drill into the repo (enter) or switch to flat view (g) and pick a worktree")
		return
	}
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
// cursor row; if the board is empty, falls back to the only registered
// repo (so you can bootstrap from zero worktrees) and errors otherwise.
func (m *Model) startAddName() {
	if m.mode == ViewGrouped {
		repos := m.visibleRepos()
		if len(repos) > 0 && m.cursor < len(repos) {
			m.inputTarget = worktreeRow{wt: domain.Worktree{Repo: repos[m.cursor].name}}
			m.input = inputAddName
			m.inputBuf = ""
			m.err = nil
			return
		}
	} else {
		visible := m.visibleRows()
		if len(visible) > 0 {
			m.inputTarget = visible[m.cursor]
			m.input = inputAddName
			m.inputBuf = ""
			m.err = nil
			return
		}
	}
	switch len(m.repos) {
	case 0:
		m.err = errors.New("no repos registered. press R to register one")
		return
	case 1:
		m.inputTarget = worktreeRow{wt: domain.Worktree{Repo: m.repos[0].Name}}
		m.input = inputAddName
		m.inputBuf = ""
		m.err = nil
	default:
		m.err = errors.New("multiple repos registered; create a worktree from the shell with `tower add --repo <name> <name>`")
	}
}

// startAddRepoPath opens the prompt for `R`: type a path (or empty for
// cwd) to register as a repo with tower.
func (m *Model) startAddRepoPath() {
	m.input = inputAddRepoPath
	m.inputBuf = ""
	m.err = nil
}

// toggleSelectionAtCursor flips the cursor row in the selection set.
// Flat view only — selection is a worktree-level concept; in grouped
// view the cursor points at a repo summary, which doesn't have a
// (repo, branch) identity to toggle.
func (m *Model) toggleSelectionAtCursor() {
	if m.mode != ViewFlat {
		m.err = errors.New("space (select) only works in flat view — press g first")
		return
	}
	visible := m.visibleRows()
	if m.cursor < 0 || m.cursor >= len(visible) {
		return
	}
	if m.selected == nil {
		m.selected = map[wtKey]bool{}
	}
	k := keyOf(visible[m.cursor])
	if m.selected[k] {
		delete(m.selected, k)
	} else {
		m.selected[k] = true
	}
	// Advance cursor so holding space sweeps down a list.
	if m.cursor < len(visible)-1 {
		m.cursor++
	}
}

// startMultiDeleteConfirm opens the y/N prompt for `D`. Captures the
// currently-selected rows now (rather than at confirm time) so a
// background reload can't shift the snapshot under the user.
func (m *Model) startMultiDeleteConfirm() (tea.Model, tea.Cmd) {
	if m.mode != ViewFlat {
		m.err = errors.New("bulk delete (D) only works in flat view — press g first")
		return m, nil
	}
	if len(m.selected) == 0 {
		m.err = errors.New("nothing selected — press space on rows to mark them, then D")
		return m, nil
	}
	m.input = inputConfirmDeleteMulti
	m.err = nil
	return m, nil
}

// startConfirmDelete opens the y/N confirmation for `d`. Refuses on
// the main worktree of a repo (path equals repo's registered path) so
// you can't accidentally tear down the primary checkout.
func (m *Model) startConfirmDelete() {
	if m.mode == ViewGrouped {
		m.err = errors.New("d removes a worktree — drill into the repo (enter) or switch to flat view (g) and pick one")
		return
	}
	dbg.Printf("startConfirmDelete: cursor=%d rows=%d filter=%q", m.cursor, len(m.rows), m.filter)
	m.info = ""
	visible := m.visibleRows()
	dbg.Printf("startConfirmDelete: visible=%d", len(visible))
	if len(visible) == 0 {
		m.err = errors.New("no worktree to remove (no rows visible)")
		dbg.Printf("startConfirmDelete: early-return (no visible rows)")
		return
	}
	if m.cursor < 0 || m.cursor >= len(visible) {
		m.err = errors.New("cursor is off the visible list; press j/k to move it onto a row")
		dbg.Printf("startConfirmDelete: early-return (cursor=%d out of range)", m.cursor)
		return
	}
	row := visible[m.cursor]
	dbg.Printf("startConfirmDelete: target row repo=%q branch=%q path=%q", row.wt.Repo, row.wt.Branch, row.wt.Path)
	repo, err := m.store.GetRepo(m.ctx, row.wt.Repo)
	if err != nil {
		m.err = err
		dbg.Printf("startConfirmDelete: GetRepo error: %v", err)
		return
	}
	if repo != nil {
		dbg.Printf("startConfirmDelete: repo.Path=%q", repo.Path)
	}
	// Compare via os.SameFile so different surface forms of the same
	// directory (forward vs backward slashes, 8.3 short names like
	// MICHAE~1 vs the long form, drive-letter casing) all collapse to
	// "same dir". String equality misses these on Windows, which
	// silently opened the confirm prompt on the main worktree.
	if repo != nil && samePath(repo.Path, row.wt.Path) {
		m.err = fmt.Errorf("refusing to remove main worktree of %s (path %s); pick a non-main row", row.wt.Repo, row.wt.Path)
		dbg.Printf("startConfirmDelete: refusing (main worktree)")
		return
	}
	m.inputTarget = row
	m.input = inputConfirmDelete
	m.err = nil
	dbg.Printf("startConfirmDelete: prompt opened, awaiting y/N")
}

func (m *Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.input {
	case inputConfirmDelete:
		return m.handleConfirmKey(msg)
	case inputConfirmDeleteMulti:
		return m.handleMultiConfirmKey(msg)
	case inputNone, inputAddName, inputAddRepoPath:
		return m.handleTextInputKey(msg)
	}
	return m, nil
}

// handleMultiConfirmKey is the y/N handler for `D` (bulk delete).
// Snapshots the selected rows from the live row set at confirm time
// so partial reloads can't leak rows in or out of the batch.
func (m *Model) handleMultiConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		targets := m.selectedRowsSnapshot()
		m.input = inputNone
		if len(targets) == 0 {
			m.err = errors.New("selection emptied before confirm")
			return m, nil
		}
		return m, removeManyCmd(m.ctx, m.workflow, targets)
	case "n", "N", "esc", "enter":
		m.input = inputNone
	}
	return m, nil
}

// selectedRowsSnapshot returns the worktreeRows whose key is in the
// selection set, scanning the live row set so the rows carry up-to-date
// dirty/PR/etc. metadata.
func (m *Model) selectedRowsSnapshot() []worktreeRow {
	out := make([]worktreeRow, 0, len(m.selected))
	for _, r := range m.rows {
		if m.selected[keyOf(r)] {
			out = append(out, r)
		}
	}
	return out
}

func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	dbg.Printf("handleConfirmKey: got key=%q (input=%d)", msg.String(), m.input)
	switch msg.String() {
	case "y", "Y":
		target := m.inputTarget
		m.input = inputNone
		// Pass --force to git when the worktree has uncommitted
		// changes — the user already saw the "(dirty)" warning in
		// the prompt and confirmed.
		force := target.wt.Dirty
		dbg.Printf("handleConfirmKey: y confirmed; dispatching removeCmd repo=%q branch=%q force=%v", target.wt.Repo, target.wt.Branch, force)
		return m, removeCmd(m.ctx, m.workflow, target.wt.Repo, target.wt.Branch, force)
	case "n", "N", "esc", "enter":
		dbg.Printf("handleConfirmKey: canceled")
		m.input = inputNone
	default:
		dbg.Printf("handleConfirmKey: ignored key %q (need y/Y to confirm)", msg.String())
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
	case inputAddRepoPath:
		m.input = inputNone
		path := buf
		if path == "" {
			top, err := gitTopLevel(m.ctx)
			if err != nil {
				m.err = fmt.Errorf("infer cwd repo (cd into a git repo or type a path): %w", err)
				return m, nil
			}
			path = top
		}
		return m, addRepoCmd(m.ctx, m.workflow, path)
	case inputNone, inputConfirmDelete, inputConfirmDeleteMulti:
		// not reachable here (filtered by handleInputKey).
	}
	return m, nil
}

// gitTopLevel returns the repo root for the current working directory.
// Used by `r` when the user submits an empty path.
func gitTopLevel(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// firstLine returns the first newline-delimited line of err.Error(),
// trimmed of trailing whitespace. Used to keep multi-line git stderr
// (the "hint:" suffix on `git branch -d` failures, etc.) out of the
// single-line error footer.
func firstLine(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, " \t\r")
}

// samePath reports whether two paths refer to the same directory on
// disk, robust to surface-form differences: separator style, Windows
// 8.3 short names (FOO~1 vs FooBarBaz), drive-letter casing. Falls
// back to filepath.Clean string equality if either path is missing.
func samePath(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(fa, fb)
}
