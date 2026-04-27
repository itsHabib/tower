package tui

import (
	"fmt"
	"strings"
	"time"
)

const (
	colRepo   = 14
	colBranch = 26
	colDirty  = 5
	colAB     = 7
	colPR     = 14
	colCI     = 22
	colRev    = 22
	colLast   = 30
)

// View renders the current model to a string.
func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	if m.helpVisible {
		return m.viewHelp()
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
  g              toggle grouped / flat
  ?              toggle this help

ACTIONS
  enter          cd into cursor row's worktree (exits tower)
  a              add a worktree in the cursor row's repo (or the only repo
                 if the board is empty)
  r              register a repo with tower (path, empty for cwd)
  d              remove cursor row's worktree (deletes branch only if
                 it's fully merged; refuses main worktree)
  o              open cursor row's PR in browser
  c              spawn claude with a new worktree
                 3 prompts: [1/3] terminal-or-background → [2/3] worktree
                 name → [3/3] initial prompt (required for background)

SYNC
  s              sync from git + GitHub (reconcile + PR/CI/reviews)

QUIT
  q / ctrl+c     quit

A/B column = commits Ahead / Behind the branch's upstream.

Press ? or esc to dismiss.`

func (m *Model) viewHelp() string {
	return titleStyle.Render(helpText)
}

func (m *Model) viewHeader() string {
	title := titleStyle.Render("tower")
	mode := "grouped"
	if m.mode == ViewFlat {
		mode = "flat"
	}
	hint := dimStyle.Render(fmt.Sprintf("[?] help  [q] quit  [s] sync  [g] %s  [/] filter  [enter] cd  [a] worktree  [r] repo  [c] claude+wt  · auto-refresh %ds", mode, int(AutoRefreshInterval.Seconds())))
	syncState := ""
	if m.syncing {
		syncState = pendingStyle.Render("◯ syncing…")
	}
	out := fmt.Sprintf("%s  %s\n%s", title, syncState, hint)
	if m.filter != "" || m.filtering {
		out += "\n" + m.viewFilterLine()
	}
	if m.input != inputNone {
		out += "\n" + m.viewInputLine()
	}
	return out
}

func (m *Model) viewInputLine() string {
	switch m.input {
	case inputAddName:
		return cursorStyle.Render(fmt.Sprintf("add worktree to %s — name: %s_", m.inputTarget.wt.Repo, m.inputBuf))
	case inputAddRepoPath:
		return cursorStyle.Render(fmt.Sprintf("register repo — path to repo dir (e.g. ../roxiq, /abs/path; empty=cwd): %s_", m.inputBuf))
	case inputClaudeSpawnMode:
		return cursorStyle.Render(fmt.Sprintf("claude+worktree in %s — [1/3] [t]erminal (new tab) or [b]ackground (headless)? esc to cancel", m.inputTarget.wt.Repo))
	case inputClaudeName:
		return cursorStyle.Render(fmt.Sprintf("claude+worktree (%s) — [2/3] worktree name: %s_", m.spawnTargetLabel(), m.inputBuf))
	case inputClaudePrompt:
		return cursorStyle.Render(fmt.Sprintf("claude+worktree (%s) — [3/3] initial prompt for %s%s: %s_", m.spawnTargetLabel(), m.stagedName, m.promptHint(), m.inputBuf))
	case inputConfirmDelete:
		return cursorStyle.Render(fmt.Sprintf("remove worktree %s/%s (and delete branch if merged)? [y/N]", m.inputTarget.wt.Repo, m.inputTarget.wt.Branch))
	case inputNone:
	}
	return ""
}

func (m *Model) spawnTargetLabel() string {
	if m.spawnTarget == SpawnBackground {
		return "background"
	}
	return "terminal"
}

func (m *Model) promptHint() string {
	if m.spawnTarget == SpawnBackground {
		return " (required)"
	}
	return " (enter to skip)"
}

func (m *Model) viewFilterLine() string {
	visible := len(m.visibleRows())
	suffix := ""
	if m.filtering {
		suffix = "_"
	}
	label := fmt.Sprintf("filter: %s%s  (%d of %d)", m.filter, suffix, visible, len(m.rows))
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
	b.WriteString(headerStyle.Render(flatHeader()))
	b.WriteString("\n")
	for i, r := range visible {
		line := formatFlatRow(r)
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

func (m *Model) viewGrouped() string {
	visible := m.visibleRows()
	groups := groupByRepo(visible)
	var b strings.Builder
	idx := 0
	for gi, repo := range groups.order {
		if gi > 0 {
			b.WriteString("\n")
		}
		b.WriteString(titleStyle.Render(repo))
		b.WriteString("\n")
		b.WriteString("  ")
		b.WriteString(headerStyle.Render(groupedHeader()))
		b.WriteString("\n")
		for _, r := range groups.byRepo[repo] {
			line := formatGroupedRow(r)
			line = stylePriority(r.priority, line)
			prefix := "  "
			if idx == m.cursor {
				prefix = cursorStyle.Render("> ")
				line = cursorStyle.Render(line)
			}
			b.WriteString(prefix)
			b.WriteString(line)
			b.WriteString("\n")
			idx++
		}
	}
	return b.String()
}

type repoGroups struct {
	order  []string
	byRepo map[string][]worktreeRow
}

func groupByRepo(rows []worktreeRow) repoGroups {
	g := repoGroups{byRepo: map[string][]worktreeRow{}}
	for _, r := range rows {
		if _, ok := g.byRepo[r.wt.Repo]; !ok {
			g.order = append(g.order, r.wt.Repo)
		}
		g.byRepo[r.wt.Repo] = append(g.byRepo[r.wt.Repo], r)
	}
	return g
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

func groupedHeader() string {
	return fmt.Sprintf("%s %s %s %s %s %s %s",
		padRight("BRANCH", colBranch),
		padRight("DIRTY", colDirty),
		padRight("A/B", colAB),
		padRight("PR", colPR),
		padRight("CI", colCI),
		padRight("REVIEWS", colRev),
		"LAST",
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

func (m *Model) viewFooter() string {
	parts := []string{fmt.Sprintf("%d worktrees", len(m.rows))}
	dirty := 0
	for _, r := range m.rows {
		if r.wt.Dirty {
			dirty++
		}
	}
	if len(m.repos) > 0 {
		parts = append(parts, fmt.Sprintf("%d repos", len(m.repos)))
	}
	if dirty > 0 {
		parts = append(parts, fmt.Sprintf("%d dirty", dirty))
	}
	if !m.lastSync.IsZero() {
		parts = append(parts, fmt.Sprintf("synced %s ago", time.Since(m.lastSync).Round(time.Second)))
	}
	footer := dimStyle.Render(strings.Join(parts, "  ·  "))
	visible := m.visibleRows()
	if len(visible) > 0 && m.cursor >= 0 && m.cursor < len(visible) {
		footer += "\n" + dimStyle.Render(visible[m.cursor].wt.Path)
	}
	if m.info != "" {
		footer += "\n" + infoStyle.Render(m.info)
	}
	if m.err != nil {
		footer += "\n" + errStyle.Render("error: "+m.err.Error())
	}
	return footer
}
