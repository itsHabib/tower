package discover

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/itsHabib/tower/internal/domain"
)

// Scan reads markdown files from dir and returns them as Tasks.
// Pure: no side effects beyond filesystem reads.
func Scan(dir string) ([]domain.Task, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir: %w", err)
	}
	var out []domain.Task
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t, err := readTask(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, t)
	}
	return out, nil
}

func readTask(path string) (domain.Task, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Task{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return domain.Task{}, err
	}
	return parseTask(path, data, info.ModTime()), nil
}

func parseTask(path string, data []byte, mtime time.Time) domain.Task {
	body, fm := splitFrontmatter(data)
	now := mtime.UTC()
	return domain.Task{
		ID:        deriveID(path, fm),
		Title:     deriveTitle(body, path, fm),
		Brief:     string(body),
		Path:      path,
		Deps:      deriveDeps(fm),
		Status:    deriveStatus(fm),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func splitFrontmatter(data []byte) (body []byte, fm map[string]string) {
	fm = map[string]string{}
	switch {
	case bytes.HasPrefix(data, []byte("---\n")):
		data = data[len("---\n"):]
	case bytes.HasPrefix(data, []byte("---\r\n")):
		data = data[len("---\r\n"):]
	default:
		return data, fm
	}
	end := bytes.Index(data, []byte("\n---\n"))
	skip := len("\n---\n")
	if end == -1 {
		end = bytes.Index(data, []byte("\r\n---\r\n"))
		skip = len("\r\n---\r\n")
	}
	if end == -1 {
		return data, fm
	}
	fmBlock := data[:end]
	body = data[end+skip:]
	for _, line := range strings.Split(string(fmBlock), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx == -1 {
			continue
		}
		fm[strings.TrimSpace(line[:idx])] = strings.TrimSpace(line[idx+1:])
	}
	return body, fm
}

func deriveID(path string, fm map[string]string) string {
	if id := fm["id"]; id != "" {
		return id
	}
	return strings.TrimSuffix(filepath.Base(path), ".md")
}

func deriveTitle(body []byte, path string, fm map[string]string) string {
	if title := fm["title"]; title != "" {
		return title
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "#"))
		}
	}
	return strings.TrimSuffix(filepath.Base(path), ".md")
}

func deriveDeps(fm map[string]string) []string {
	raw := fm["deps"]
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func deriveStatus(fm map[string]string) domain.Status {
	if s := fm["status"]; s != "" {
		return domain.Status(s)
	}
	return domain.StatusDraft
}
