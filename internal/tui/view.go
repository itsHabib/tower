package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	colRepo       = 14
	colBranch     = 26
	colDirty      = 5
	colAB         = 7
	colPR         = 14
	colCI         = 22
	colRev        = 22
	colLast       = 30
	colRepoName   = 20
	colWorktrees  = 9
	colDirtyCount = 7
	colOpenPRs    = 8
	colFailingCI  = 10
)

// View renders the current model to a string.
func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.helpVisible {
		return m.viewHelp()
	}
	if m.detailRow != nil {
		return m.viewDetail(*m.detailRow)
	}
	var b strings.Builder
	b.WriteString(m.viewHeader())
	b.WriteString("\n\n")
	b.WriteString(m.viewBody())
	b.WriteString("\n")
	b.WriteString(m.viewFooter())
	return b.String()
}

const helpText = `TOWER

NAVIGATION
  j / down       next row
  k / up         previous row
  /              filter (substring on branch / repo / title)
  esc            clear filter

VIEW
  g              toggle grouped (one row per repo) / flat (one row per worktree)
  ?              toggle this help

ACTIONS
  enter          grouped: drill into cursor repo (filter to its worktrees)
                 flat:    open detail panel for cursor row
  a              add a worktree in the cursor row's repo (or the only repo
                 if the board is empty)
  r              register a repo with tower (path, empty for cwd)
  d              flat only — remove cursor row's worktree (deletes branch
                 only if it's fully merged; refuses main worktree)
  D              flat only — remove every selected worktree (see space)
  o              flat only — open cursor row's PR in browser

SELECTION (flat only)
  space          toggle selection on the cursor row, then advance cursor
  esc            clear filter and selection

DETAIL PANEL (flat enter)
  esc / q / enter   close the panel
  o                 open the row's PR in browser

SYNC
  s              sync from git + GitHub (reconcile + PR/CI/reviews)

QUIT
  q / ctrl+c     quit

A/B column = commits Ahead / Behind the branch's upstream.

Press ? or esc to dismiss.`

func (m *Model) viewHelp() string {
	return titleStyle.Render(helpText)
}

// viewDetail renders the full state of one worktree row: identity,
// path, dirty/upstream status, latest commit, and any GitHub state
// (PR / CI / reviews) the store knows about. Opened with enter on a
// flat-view row; dismissed with esc / q / enter.
func (m *Model) viewDetail(r worktreeRow) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(fmt.Sprintf("%s / %s", r.wt.Repo, r.wt.Branch)))
	b.WriteString("\n\n")

	dirty := "clean"
	if r.wt.Dirty {
		dirty = errStyle.Render("dirty (uncommitted changes)")
	}
	rows := [][2]string{
		{"path", r.wt.Path},
		{"dirty", dirty},
		{"ahead/behind", fmt.Sprintf("%d / %d", r.wt.Ahead, r.wt.Behind)},
		{"head", shortHEAD(r.wt.HEAD)},
		{"last commit", formatLast(r.wt.LastCommit, r.wt.Title)},
	}
	for _, kv := range rows {
		b.WriteString(detailLine(kv[0], kv[1]))
	}

	b.WriteString("\n")
	b.WriteString(headerStyle.Render("PR"))
	b.WriteString("\n")
	if r.pr == nil {
		b.WriteString(dimStyle.Render("  (none)"))
		b.WriteString("\n")
	} else {
		b.WriteString(detailLine("number", fmt.Sprintf("#%d", r.pr.Number)))
		b.WriteString(detailLine("state", string(r.pr.State)))
		if r.pr.Title != "" {
			b.WriteString(detailLine("title", r.pr.Title))
		}
		if r.pr.URL != "" {
			b.WriteString(detailLine("url", r.pr.URL))
		}
	}

	b.WriteString("\n")
	b.WriteString(headerStyle.Render("CI"))
	b.WriteString("\n")
	if len(r.checks) == 0 {
		b.WriteString(dimStyle.Render("  (no checks)"))
		b.WriteString("\n")
	} else {
		for _, c := range r.checks {
			b.WriteString(detailLine(string(c.Conclusion), c.Name))
		}
	}

	b.WriteString("\n")
	b.WriteString(headerStyle.Render("REVIEWS"))
	b.WriteString("\n")
	latest := latestPerReviewer(r.reviews)
	if len(latest) == 0 {
		b.WriteString(dimStyle.Render("  (no reviews)"))
		b.WriteString("\n")
	} else {
		for reviewer, state := range latest {
			b.WriteString(detailLine(reviewer, string(state)))
		}
	}

	b.WriteString("\n")
	hint := "[esc/q/enter] close"
	if r.pr != nil && r.pr.URL != "" {
		hint += "  [o] open PR in browser"
	}
	b.WriteString(dimStyle.Render(hint))
	return b.String()
}

func detailLine(label, value string) string {
	return fmt.Sprintf("  %s  %s\n", dimStyle.Render(padRight(label, 14)), value)
}

// shortHEAD trims the HEAD sha to 8 chars for display; "-" if empty.
func shortHEAD(sha string) string {
	if sha == "" {
		return "-"
	}
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func (m *Model) viewHeader() string {
	title := titleStyle.Render("tower")
	mode := "grouped"
	if m.mode == ViewFlat {
		mode = "flat"
	}
	// Two-line keybinding hint. Top row = view-level controls that
	// always work; bottom row = actions whose meaning depends on the
	// current mode. Splitting them keeps the action row honest about
	// which keys actually do something *right now*.
	nav := dimStyle.Render(fmt.Sprintf("[?] help  [q] quit  [s] sync  [g] %s  [/] filter  · auto-refresh %ds", mode, int(AutoRefreshInterval.Seconds())))
	actions := dimStyle.Render(m.actionHint())
	syncState := ""
	if m.syncing {
		syncState = pendingStyle.Render("◯ syncing…")
	}
	out := fmt.Sprintf("%s  %s\n%s\n%s", title, syncState, nav, actions)
	if m.filter != "" || m.filtering {
		out += "\n" + m.viewFilterLine()
	}
	if m.input != inputNone {
		out += "\n" + m.viewInputLine()
	}
	return out
}

// actionHint returns the mode-specific second hint line. Grouped mode
// hides the worktree-only keys (d/D/o/space) — they error out there
// and showing them would be misleading.
func (m *Model) actionHint() string {
	if m.mode == ViewGrouped {
		return "[enter] drill  [a] worktree  [r] repo"
	}
	return "[enter] details  [a] worktree  [r] repo  [d] remove  [o] open PR  [space] select  [D] delete selected"
}

func (m *Model) viewInputLine() string {
	switch m.input {
	case inputAddName:
		// Two lines so the target repo is unmistakable — single-line
		// versions of this prompt got skimmed past.
		head := titleStyle.Render("→ adding worktree in " + m.inputTarget.wt.Repo)
		body := cursorStyle.Render(fmt.Sprintf("  name: %s_", m.inputBuf))
		return head + "\n" + body
	case inputAddRepoPath:
		return cursorStyle.Render(fmt.Sprintf("register repo — path to repo dir (e.g. ../roxiq, /abs/path; empty=cwd): %s_", m.inputBuf))
	case inputConfirmDelete:
		warn := ""
		if m.inputTarget.wt.Dirty {
			warn = errStyle.Render(" — DIRTY: uncommitted changes will be discarded")
		}
		return cursorStyle.Render(fmt.Sprintf("remove worktree %s/%s (and delete branch if merged)%s? [y/N]",
			m.inputTarget.wt.Repo, m.inputTarget.wt.Branch, warn))
	case inputConfirmDeleteMulti:
		dirty := 0
		for _, r := range m.selectedRowsSnapshot() {
			if r.wt.Dirty {
				dirty++
			}
		}
		warn := ""
		if dirty > 0 {
			warn = errStyle.Render(fmt.Sprintf(" — %d DIRTY (uncommitted changes discarded)", dirty))
		}
		return cursorStyle.Render(fmt.Sprintf("remove %d selected worktrees%s? [y/N]", len(m.selected), warn))
	case inputNone:
	}
	return ""
}

func (m *Model) viewFilterLine() string {
	suffix := ""
	if m.filtering {
		suffix = "_"
	}
	visible := m.visibleCount()
	total := len(m.rows)
	noun := "worktrees"
	if m.mode == ViewGrouped {
		total = len(m.repos)
		noun = "repos"
	}
	label := fmt.Sprintf("filter: %s%s  (%d of %d %s)", m.filter, suffix, visible, total, noun)
	return cursorStyle.Render(label)
}

func (m *Model) viewBody() string {
	if len(m.rows) == 0 {
		return dimStyle.Render(m.emptyHint())
	}
	if m.filter != "" && len(m.visibleRows()) == 0 {
		return dimStyle.Render(fmt.Sprintf("no worktrees match %q", m.filter))
	}
	if m.mode == ViewFlat {
		return m.viewFlat()
	}
	return m.viewGrouped()
}

func (m *Model) emptyHint() string {
	if m.lastSync.IsZero() && m.syncing {
		return "loading…"
	}
	if m.noRepos {
		return "no repos registered. press R to register one (empty path = cwd) or run `tower repo add`."
	}
	return "no worktrees yet. press a to create one (uses the only repo if there's just one), or run `tower add <name>` from a repo."
}

func (m *Model) viewFlat() string {
	visible := m.visibleRows()
	var b strings.Builder
	b.WriteString("    ") // align with the per-row "  [ ]"/"> [x]" prefix
	b.WriteString(headerStyle.Render(flatHeader()))
	b.WriteString("\n")
	for i, r := range visible {
		line := formatFlatRow(r)
		line = stylePriority(r.priority, line)
		cursorPrefix := "  "
		if i == m.cursor {
			cursorPrefix = cursorStyle.Render("> ")
			line = cursorStyle.Render(line)
		}
		selectMark := "  "
		if m.selected[keyOf(r)] {
			selectMark = cursorStyle.Render("✓ ")
		}
		b.WriteString(cursorPrefix)
		b.WriteString(selectMark)
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Model) viewGrouped() string {
	repos := m.visibleRepos()
	var b strings.Builder
	b.WriteString(headerStyle.Render(repoHeader()))
	b.WriteString("\n")
	for i, r := range repos {
		line := formatRepoRow(r)
		line = stylePriority(r.priority, line)
		prefix := "  "
		if i == m.cursor {
			prefix = cursorStyle.Render("> ")
			line = cursorStyle.Render(line)
		}
		b.WriteString(prefix)
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

func flatHeader() string {
	return fmt.Sprintf("%s %s %s %s %s %s %s %s",
		padRight("REPO", colRepo),
		padRight("BRANCH", colBranch),
		padRight("DIRTY", colDirty),
		padRight("A/B", colAB),
		padRight("PR", colPR),
		padRight("CI", colCI),
		padRight("REVIEWS", colRev),
		"LAST",
	)
}

func repoHeader() string {
	return fmt.Sprintf("%s %s %s %s %s %s",
		padRight("REPO", colRepoName),
		padRight("WORKTREES", colWorktrees),
		padRight("DIRTY", colDirtyCount),
		padRight("OPEN PRS", colOpenPRs),
		padRight("FAILING CI", colFailingCI),
		"LAST ACTIVITY",
	)
}

func formatRepoRow(r repoRow) string {
	dirty := "-"
	if r.dirty > 0 {
		dirty = strconv.Itoa(r.dirty)
	}
	openPRs := "-"
	if r.openPRs > 0 {
		openPRs = strconv.Itoa(r.openPRs)
	}
	failingCI := "-"
	if r.failingCI > 0 {
		failingCI = strconv.Itoa(r.failingCI)
	}
	last := FormatAge(r.lastCommit)
	if last == "" {
		last = "-"
	}
	return fmt.Sprintf("%s %s %s %s %s %s",
		padRight(truncate(r.name, colRepoName), colRepoName),
		padRight(strconv.Itoa(r.worktrees), colWorktrees),
		padRight(dirty, colDirtyCount),
		padRight(openPRs, colOpenPRs),
		padRight(failingCI, colFailingCI),
		last,
	)
}

func formatFlatRow(r worktreeRow) string {
	return fmt.Sprintf("%s %s",
		padRight(truncate(r.wt.Repo, colRepo), colRepo),
		formatGroupedRow(r),
	)
}

func formatGroupedRow(r worktreeRow) string {
	branch := truncate(r.wt.Branch, colBranch)
	dirty := "-"
	if r.wt.Dirty {
		dirty = "yes"
	}
	ab := fmt.Sprintf("%d/%d", r.wt.Ahead, r.wt.Behind)
	pr := "-"
	if r.pr != nil {
		pr = fmt.Sprintf("#%d %s", r.pr.Number, r.pr.State)
	}
	ci := SummarizeChecks(r.checks)
	rev := SummarizeReviews(r.reviews)
	last := formatLast(r.wt.LastCommit, r.wt.Title)
	return fmt.Sprintf("%s %s %s %s %s %s %s",
		padRight(branch, colBranch),
		padRight(dirty, colDirty),
		padRight(ab, colAB),
		padRight(truncate(pr, colPR), colPR),
		padRight(truncate(ci, colCI), colCI),
		padRight(truncate(rev, colRev), colRev),
		truncate(last, colLast),
	)
}

func formatLast(t time.Time, subject string) string {
	age := FormatAge(t)
	switch {
	case age == "" && subject == "":
		return "-"
	case age == "":
		return subject
	case subject == "":
		return age
	default:
		return age + " · " + subject
	}
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func (m *Model) viewFooter() string {
	parts := []string{pluralize(len(m.rows), "worktree", "worktrees")}
	dirty := 0
	for _, r := range m.rows {
		if r.wt.Dirty {
			dirty++
		}
	}
	if len(m.repos) > 0 {
		parts = append(parts, pluralize(len(m.repos), "repo", "repos"))
	}
	if dirty > 0 {
		parts = append(parts, fmt.Sprintf("%d dirty", dirty))
	}
	if n := len(m.selected); n > 0 {
		parts = append(parts, cursorStyle.Render(fmt.Sprintf("%d selected — D to delete", n)))
	}
	if !m.lastSync.IsZero() {
		parts = append(parts, fmt.Sprintf("synced %s ago", time.Since(m.lastSync).Round(time.Second)))
	}
	footer := dimStyle.Render(strings.Join(parts, "  ·  "))
	if path := m.cursorPath(); path != "" {
		footer += "\n" + dimStyle.Render(path)
	}
	if m.info != "" {
		footer += "\n" + infoStyle.Render(m.info)
	}
	if m.err != nil {
		footer += "\n" + errStyle.Render("error: "+m.err.Error())
	}
	return footer
}
