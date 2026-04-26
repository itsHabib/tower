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
	colPR     = 16
	colCI     = 22
	colRev    = 22
)

// View renders the current model to a string.
func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(m.viewHeader())
	b.WriteString("\n\n")
	b.WriteString(m.viewBody())
	b.WriteString("\n")
	b.WriteString(m.viewFooter())
	return b.String()
}

func (m *Model) viewHeader() string {
	title := titleStyle.Render("tower")
	mode := "grouped"
	if m.mode == ViewFlat {
		mode = "flat"
	}
	hint := dimStyle.Render(fmt.Sprintf("[q] quit  [s] sync  [r] reload  [g] %s view  [↑/↓] move  [enter] open", mode))
	syncState := ""
	if m.syncing {
		syncState = pendingStyle.Render("◯ syncing…")
	}
	return fmt.Sprintf("%s  %s\n%s", title, syncState, hint)
}

func (m *Model) viewBody() string {
	if len(m.rows) == 0 {
		return dimStyle.Render("no worktrees tracked. register a repo with `tower repo add` and create one with `tower add <name>`.")
	}
	if m.mode == ViewFlat {
		return m.viewFlat()
	}
	return m.viewGrouped()
}

func (m *Model) viewFlat() string {
	var b strings.Builder
	b.WriteString(headerStyle.Render(flatHeader()))
	b.WriteString("\n")
	for i, r := range m.rows {
		line := formatFlatRow(r)
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
	groups := groupByRepo(m.rows)
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
		"PATH",
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
		"PATH",
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
	return fmt.Sprintf("%s %s %s %s %s %s %s",
		padRight(branch, colBranch),
		padRight(dirty, colDirty),
		padRight(ab, colAB),
		padRight(truncate(pr, colPR), colPR),
		padRight(truncate(ci, colCI), colCI),
		padRight(truncate(rev, colRev), colRev),
		r.wt.Path,
	)
}

func (m *Model) viewFooter() string {
	parts := []string{fmt.Sprintf("%d worktrees", len(m.rows))}
	dirty := 0
	repos := map[string]bool{}
	for _, r := range m.rows {
		repos[r.wt.Repo] = true
		if r.wt.Dirty {
			dirty++
		}
	}
	if len(repos) > 1 {
		parts = append(parts, fmt.Sprintf("%d repos", len(repos)))
	}
	if dirty > 0 {
		parts = append(parts, fmt.Sprintf("%d dirty", dirty))
	}
	if !m.lastSync.IsZero() {
		parts = append(parts, fmt.Sprintf("synced %s ago", time.Since(m.lastSync).Round(time.Second)))
	}
	footer := dimStyle.Render(strings.Join(parts, "  ·  "))
	if m.err != nil {
		footer += "\n" + errStyle.Render("error: "+m.err.Error())
	}
	return footer
}
