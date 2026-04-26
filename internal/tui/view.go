package tui

import (
	"fmt"
	"strings"
	"time"
)

const (
	colBranch = 28
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
	b.WriteString(m.viewTable())
	b.WriteString("\n")
	b.WriteString(m.viewFooter())
	return b.String()
}

func (m *Model) viewHeader() string {
	title := titleStyle.Render("tower")
	hint := dimStyle.Render("[q] quit  [s] sync  [r] reload  [↑/↓] move  [enter] open")
	syncState := ""
	if m.syncing {
		syncState = pendingStyle.Render("◯ syncing…")
	}
	return fmt.Sprintf("%s  %s\n%s", title, syncState, hint)
}

func (m *Model) viewTable() string {
	if len(m.rows) == 0 {
		return dimStyle.Render("no worktrees tracked. create one with `tower add <name>`.")
	}
	var b strings.Builder
	b.WriteString(headerStyle.Render(headerRow()))
	b.WriteString("\n")
	for i, r := range m.rows {
		line := formatRow(r)
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

func headerRow() string {
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

func formatRow(r worktreeRow) string {
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
	for _, r := range m.rows {
		if r.wt.Dirty {
			dirty++
		}
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
