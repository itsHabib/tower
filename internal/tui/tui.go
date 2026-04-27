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

// Input modes for one-shot prompts (a/R/d/c). inputNone = no prompt active.
const (
	inputNone            inputMode = iota
	inputAddName                   // typing a name for `a` (new tower-style worktree)
	inputAddRepoPath               // typing a path for `R` (register a repo)
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

func removeCmd(ctx context.Context, wf *workflow.Service, repoName, name string) tea.Cmd {
	return func() tea.Msg {
		dbg.Printf("removeCmd: calling wf.Remove(repo=%q, name=%q)", repoName, name)
		err := wf.Remove(ctx, repoName, name)
		if err != nil {
			dbg.Printf("removeCmd: wf.Remove returned err: %v", err)
		} else {
			dbg.Printf("removeCmd: wf.Remove returned nil (success)")
		}
		return removedMsg{err: err}
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

// Update routes incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case loadedMsg:
		m.rows = msg.rows
		m.repos = msg.repos
		m.noRepos = msg.noRepos
		// Only overwrite m.err if the load itself failed. Plain
		// "no error" reloads (the common case after every mutation
		// or auto-refresh tick) must not clobber a freshly-set
		// error from the action that triggered them — that wiped
		// errors before the user could read them.
		if msg.err != nil {
			m.err = msg.err
		}
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
		dbg.Printf("Update: removedMsg arrived (err=%v)", msg.err)
		return m.afterMutation(msg.err)
	case repoAddedMsg:
		m.info = ""
		if msg.err != nil {
			m.err = msg.err
			return m, loadCmd(m.ctx, m.workflow, m.store)
		}
		m.err = nil
		if msg.repo != nil {
			m.info = fmt.Sprintf("registered %s at %s — syncing to pick up worktrees…", msg.repo.Name, msg.repo.Path)
		}
		// Kick a sync now so the new repo's worktrees show up without
		// waiting on the 60s auto-refresh tick — that wait is what
		// reads as "nothing happened" the first time you press R.
		if !m.syncing {
			m.syncing = true
			return m, tea.Batch(syncCmd(m.ctx, m.workflow), loadCmd(m.ctx, m.workflow, m.store))
		}
		return m, loadCmd(m.ctx, m.workflow, m.store)
	}
	return m, nil
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
	dbg.Printf("handleActionKey: %q (input=%d filter=%q filtering=%v helpVisible=%v)", msg.String(), m.input, m.filter, m.filtering, m.helpVisible)
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "s":
		if !m.syncing {
			m.syncing = true
			return m, syncCmd(m.ctx, m.workflow)
		}
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
	case "r":
		m.startAddRepoPath()
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
// cursor row; if the board is empty, falls back to the only registered
// repo (so you can bootstrap from zero worktrees) and errors otherwise.
func (m *Model) startAddName() {
	visible := m.visibleRows()
	if len(visible) > 0 {
		m.inputTarget = visible[m.cursor]
		m.input = inputAddName
		m.inputBuf = ""
		m.err = nil
		return
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
	case inputClaudeSpawnMode:
		return m.handleSpawnModeKey(msg)
	case inputNone, inputAddName, inputAddRepoPath, inputClaudeName, inputClaudePrompt:
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
	dbg.Printf("handleConfirmKey: got key=%q (input=%d)", msg.String(), m.input)
	switch msg.String() {
	case "y", "Y":
		target := m.inputTarget
		m.input = inputNone
		dbg.Printf("handleConfirmKey: y confirmed; dispatching removeCmd repo=%q branch=%q", target.wt.Repo, target.wt.Branch)
		return m, removeCmd(m.ctx, m.workflow, target.wt.Repo, target.wt.Branch)
	case "n", "N", "esc", "enter":
		dbg.Printf("handleConfirmKey: cancelled")
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

// gitTopLevel returns the repo root for the current working directory.
// Used by `r` when the user submits an empty path.
func gitTopLevel(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
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
