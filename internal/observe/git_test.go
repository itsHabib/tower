package observe

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestParseWorktreeList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Worktree
	}{
		{
			name: "single main",
			in: "worktree /repo\n" +
				"HEAD abc123\n" +
				"branch refs/heads/main\n",
			want: []Worktree{{Path: "/repo", HEAD: "abc123", Branch: "main"}},
		},
		{
			name: "main plus feature worktrees",
			in: "worktree /repo\n" +
				"HEAD abc\n" +
				"branch refs/heads/main\n\n" +
				"worktree /repo/.worktrees/feat-x\n" +
				"HEAD def\n" +
				"branch refs/heads/tower/feat-x\n",
			want: []Worktree{
				{Path: "/repo", HEAD: "abc", Branch: "main"},
				{Path: "/repo/.worktrees/feat-x", HEAD: "def", Branch: "tower/feat-x"},
			},
		},
		{
			name: "detached worktree has no branch",
			in: "worktree /repo\nHEAD abc\nbranch refs/heads/main\n\n" +
				"worktree /repo/.worktrees/spike\nHEAD def\ndetached\n",
			want: []Worktree{
				{Path: "/repo", HEAD: "abc", Branch: "main"},
				{Path: "/repo/.worktrees/spike", HEAD: "def"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWorktreeList([]byte(tt.in))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("want %+v\ngot %+v", tt.want, got)
			}
		})
	}
}

type fakeRunner struct {
	last struct {
		dir, name string
		args      []string
	}
	out  []byte
	err  error
	hook func(name string, args []string) ([]byte, error)
}

func (f *fakeRunner) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	f.last.dir = dir
	f.last.name = name
	f.last.args = args
	if f.hook != nil {
		return f.hook(name, args)
	}
	return f.out, f.err
}

func TestGitObserverWorktrees(t *testing.T) {
	r := &fakeRunner{out: []byte("worktree /repo\nHEAD abc\nbranch refs/heads/main\n")}
	g := &GitObserver{Repo: "/repo", Runner: r}
	wts, err := g.Worktrees(context.Background())
	if err != nil {
		t.Fatalf("worktrees: %v", err)
	}
	if len(wts) != 1 || wts[0].Branch != "main" {
		t.Fatalf("unexpected worktrees: %+v", wts)
	}
}

func TestAddRemoveWorktree(t *testing.T) {
	r := &fakeRunner{}
	g := &GitObserver{Repo: "/repo", Runner: r}
	if err := g.AddWorktree(context.Background(), "/p", "tower/x"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if !reflect.DeepEqual(r.last.args, []string{"worktree", "add", "-b", "tower/x", "/p"}) {
		t.Fatalf("add args: %v", r.last.args)
	}
	if err := g.RemoveWorktree(context.Background(), "/p"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !reflect.DeepEqual(r.last.args, []string{"worktree", "remove", "/p"}) {
		t.Fatalf("remove args: %v", r.last.args)
	}
}

func TestDeleteBranchUsesSafeDelete(t *testing.T) {
	r := &fakeRunner{}
	g := &GitObserver{Repo: "/repo", Runner: r}
	if err := g.DeleteBranch(context.Background(), "tower/x"); err != nil {
		t.Fatalf("delete branch: %v", err)
	}
	// -d (lowercase) refuses if the branch is unmerged — that's the
	// guarantee we want so we don't silently lose work on remove.
	if !reflect.DeepEqual(r.last.args, []string{"branch", "-d", "tower/x"}) {
		t.Fatalf("args: %v", r.last.args)
	}
}

func TestDirty(t *testing.T) {
	cases := []struct {
		name string
		out  []byte
		want bool
	}{
		{"clean", []byte(""), false},
		{"clean whitespace", []byte("\n  \n"), false},
		{"modified", []byte(" M file.go\n"), true},
		{"new file", []byte("?? new.go\n"), true},
		// Tower's own worktree dir shows up as untracked in the main
		// worktree; filter it out so main isn't perma-dirty.
		{"only worktrees dir untracked", []byte("?? .worktrees/\n"), false},
		{"worktrees dir plus real dirt", []byte("?? .worktrees/\n M file.go\n"), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &fakeRunner{out: c.out}
			g := &GitObserver{Repo: "/repo", Runner: r}
			got, err := g.Dirty(context.Background(), "/wt")
			if err != nil {
				t.Fatalf("dirty: %v", err)
			}
			if got != c.want {
				t.Errorf("want %v got %v", c.want, got)
			}
		})
	}
}

func TestAheadBehindFromUpstream(t *testing.T) {
	r := &fakeRunner{out: []byte("2\t5\n")} // behind \t ahead
	g := &GitObserver{Repo: "/repo", Runner: r}
	ahead, behind, err := g.AheadBehind(context.Background(), "/wt")
	if err != nil {
		t.Fatalf("ahead/behind: %v", err)
	}
	if ahead != 5 || behind != 2 {
		t.Fatalf("want ahead=5 behind=2, got ahead=%d behind=%d", ahead, behind)
	}
}

func TestAheadBehindNoUpstreamFallsBackToZero(t *testing.T) {
	r := &fakeRunner{err: errors.New("no upstream")}
	g := &GitObserver{Repo: "/repo", Runner: r}
	a, b, err := g.AheadBehind(context.Background(), "/wt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if a != 0 || b != 0 {
		t.Fatalf("want 0,0 got %d,%d", a, b)
	}
}

func TestAheadBehindFallsBackToOriginHEAD(t *testing.T) {
	calls := 0
	r := &fakeRunner{hook: func(_ string, args []string) ([]byte, error) {
		calls++
		// First call (with @{u}) fails, second (origin/HEAD) succeeds
		for _, a := range args {
			if a == "@{u}...HEAD" {
				return nil, errors.New("no upstream")
			}
		}
		return []byte("0\t3\n"), nil
	}}
	g := &GitObserver{Repo: "/repo", Runner: r}
	ahead, behind, err := g.AheadBehind(context.Background(), "/wt")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
	if ahead != 3 || behind != 0 {
		t.Fatalf("want ahead=3 behind=0 got ahead=%d behind=%d", ahead, behind)
	}
}

func TestLastCommit(t *testing.T) {
	r := &fakeRunner{out: []byte("1700000000\nfix: thing\n")}
	g := &GitObserver{Repo: "/repo", Runner: r}
	ts, subj, err := g.LastCommit(context.Background(), "/wt")
	if err != nil {
		t.Fatalf("last commit: %v", err)
	}
	if subj != "fix: thing" {
		t.Errorf("subject: %q", subj)
	}
	want := time.Unix(1700000000, 0).UTC()
	if !ts.Equal(want) {
		t.Errorf("ts: want %v got %v", want, ts)
	}
}

func TestLastCommitEmptyRepo(t *testing.T) {
	r := &fakeRunner{out: []byte("")}
	g := &GitObserver{Repo: "/repo", Runner: r}
	ts, subj, err := g.LastCommit(context.Background(), "/wt")
	if err != nil {
		t.Fatalf("last commit: %v", err)
	}
	if !ts.IsZero() || subj != "" {
		t.Fatalf("expected empty, got %v %q", ts, subj)
	}
}

func TestMainRoot(t *testing.T) {
	r := &fakeRunner{out: []byte("worktree /main\nHEAD abc\nbranch refs/heads/main\n\nworktree /main/.worktrees/x\nHEAD def\nbranch refs/heads/x\n")}
	g := &GitObserver{Repo: "/anywhere", Runner: r}
	root, err := g.MainRoot(context.Background())
	if err != nil {
		t.Fatalf("main root: %v", err)
	}
	if root != "/main" {
		t.Fatalf("want /main got %q", root)
	}
}
