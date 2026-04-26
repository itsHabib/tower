package tui

import (
	"fmt"
	"strings"
	"time"
)

const (
	colID     = 22
	colStatus = 10
	colPR     = 16
	colCI     = 26
	colRev    = 24
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
	hint := dimStyle.Render("[q] quit  [s] sync  [r] reload  [↑/↓] move  [enter] open worktree")
	syncState := ""
	if m.syncing {
		syncState = pendingStyle.Render("◯ syncing…")
	}
	return fmt.Sprintf("%s  %s\n%s", title, syncState, hint)
}

func (m *Model) viewTable() string {
	if len(m.rows) == 0 {
		return dimStyle.Render("no tasks. run `tower discover` to scan features/")
	}
	var b strings.Builder
	header := headerRow()
	b.WriteString(headerStyle.Render(header))
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
	return fmt.Sprintf("%s %s %s %s %s %s",
		padRight("ID", colID),
		padRight("STATUS", colStatus),
		padRight("PR", colPR),
		padRight("CI", colCI),
		padRight("REVIEWS", colRev),
		"WORKTREE",
	)
}

func formatRow(r taskRow) string {
	id := truncate(r.task.ID, colID)
	rawStatus := string(r.task.Status)
	status := styleStatus(rawStatus, padRight(truncate(rawStatus, colStatus), colStatus))

	pr := "-"
	if r.pr != nil {
		pr = fmt.Sprintf("#%d %s", r.pr.Number, r.pr.State)
	}

	ci := SummarizeChecks(r.checks)
	rev := SummarizeReviews(r.reviews)

	wt := "-"
	if r.wt != nil {
		wt = r.wt.Path
	}

	return fmt.Sprintf("%s %s %s %s %s %s",
		padRight(id, colID),
		status,
		padRight(truncate(pr, colPR), colPR),
		padRight(truncate(ci, colCI), colCI),
		padRight(truncate(rev, colRev), colRev),
		wt,
	)
}

func (m *Model) viewFooter() string {
	parts := []string{
		fmt.Sprintf("%d tasks", len(m.rows)),
	}
	blocked := 0
	for _, r := range m.rows {
		if r.task.Status == "blocked" {
			blocked++
		}
	}
	if blocked > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", blocked))
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
