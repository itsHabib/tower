package discover

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/itsHabib/tower/internal/domain"
)

func TestScanEmptyDir(t *testing.T) {
	dir := t.TempDir()
	got, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want 0 tasks, got %d", len(got))
	}
}

func TestScanSkipsNonMarkdown(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "notes.txt"), "ignored")
	mustWrite(t, filepath.Join(dir, "feat.md"), "# Feature\nbody")
	got, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != 1 || got[0].ID != "feat" {
		t.Fatalf("unexpected tasks: %+v", got)
	}
}

func TestScanSkipsSubdirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "nested", "deep.md"), "# Deep")
	mustWrite(t, filepath.Join(dir, "top.md"), "# Top")
	got, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(got) != 1 || got[0].ID != "top" {
		t.Fatalf("expected only top-level md, got %+v", got)
	}
}

func TestParseFrontmatter(t *testing.T) {
	data := []byte("---\nid: feat-login\ntitle: SSO Login\ndeps: feat-users, feat-sessions\nstatus: active\n---\n# Heading\nbody text\n")
	got := parseTask("/repo/features/login.md", data, time.Unix(1700000000, 0))
	if got.ID != "feat-login" {
		t.Errorf("id: got %q", got.ID)
	}
	if got.Title != "SSO Login" {
		t.Errorf("title: got %q", got.Title)
	}
	if got.Status != domain.StatusActive {
		t.Errorf("status: got %s", got.Status)
	}
	if !reflect.DeepEqual(got.Deps, []string{"feat-users", "feat-sessions"}) {
		t.Errorf("deps: got %v", got.Deps)
	}
	if got.Brief != "# Heading\nbody text\n" {
		t.Errorf("brief mismatch: %q", got.Brief)
	}
}

func TestParseNoFrontmatter(t *testing.T) {
	data := []byte("# Login flow\n\nA login feature.\n")
	got := parseTask("/repo/features/login.md", data, time.Unix(1700000000, 0))
	if got.ID != "login" {
		t.Errorf("id from filename: got %q", got.ID)
	}
	if got.Title != "Login flow" {
		t.Errorf("title from H1: got %q", got.Title)
	}
	if got.Status != domain.StatusDraft {
		t.Errorf("status default: got %s", got.Status)
	}
	if len(got.Deps) != 0 {
		t.Errorf("deps default: got %v", got.Deps)
	}
}

func TestParseTitleFallsBackToFilename(t *testing.T) {
	data := []byte("no heading just text\n")
	got := parseTask("/repo/features/widget.md", data, time.Unix(1700000000, 0))
	if got.Title != "widget" {
		t.Errorf("title fallback: got %q", got.Title)
	}
}

func TestSplitFrontmatterCRLF(t *testing.T) {
	data := []byte("---\r\nid: a\r\n---\r\nbody\r\n")
	body, fm := splitFrontmatter(data)
	if fm["id"] != "a" {
		t.Errorf("id: got %q", fm["id"])
	}
	if string(body) != "body\r\n" {
		t.Errorf("body: got %q", body)
	}
}

func TestScanMultiple(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.md"), "# A")
	mustWrite(t, filepath.Join(dir, "b.md"), "---\nid: bee\n---\n# B")
	got, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	ids := make([]string, len(got))
	for i, t := range got {
		ids[i] = t.ID
	}
	sort.Strings(ids)
	want := []string{"a", "bee"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("ids: want %v got %v", want, ids)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
