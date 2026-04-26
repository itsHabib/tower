package observe

import (
	"context"
	"reflect"
	"testing"
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
				"branch refs/heads/main\n" +
				"\n" +
				"worktree /repo/.worktrees/feat-x\n" +
				"HEAD def\n" +
				"branch refs/heads/tower/feat-x\n" +
				"\n" +
				"worktree /repo/.worktrees/feat-y\n" +
				"HEAD ghi\n" +
				"branch refs/heads/tower/feat-y\n",
			want: []Worktree{
				{Path: "/repo", HEAD: "abc", Branch: "main"},
				{Path: "/repo/.worktrees/feat-x", HEAD: "def", Branch: "tower/feat-x"},
				{Path: "/repo/.worktrees/feat-y", HEAD: "ghi", Branch: "tower/feat-y"},
			},
		},
		{
			name: "detached worktree has no branch",
			in: "worktree /repo\n" +
				"HEAD abc\n" +
				"branch refs/heads/main\n" +
				"\n" +
				"worktree /repo/.worktrees/spike\n" +
				"HEAD def\n" +
				"detached\n",
			want: []Worktree{
				{Path: "/repo", HEAD: "abc", Branch: "main"},
				{Path: "/repo/.worktrees/spike", HEAD: "def"},
			},
		},
		{
			name: "empty",
			in:   "",
			want: nil,
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
	out []byte
	err error
}

func (f *fakeRunner) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	f.last.dir = dir
	f.last.name = name
	f.last.args = args
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
	if r.last.name != "git" || r.last.dir != "/repo" {
		t.Fatalf("runner not called as expected: %+v", r.last)
	}
	wantArgs := []string{"worktree", "list", "--porcelain"}
	if !reflect.DeepEqual(r.last.args, wantArgs) {
		t.Fatalf("args: want %v got %v", wantArgs, r.last.args)
	}
}

func TestAddWorktree(t *testing.T) {
	r := &fakeRunner{}
	g := &GitObserver{Repo: "/repo", Runner: r}
	if err := g.AddWorktree(context.Background(), "/repo/.worktrees/x", "tower/x"); err != nil {
		t.Fatalf("add: %v", err)
	}
	wantArgs := []string{"worktree", "add", "-b", "tower/x", "/repo/.worktrees/x"}
	if !reflect.DeepEqual(r.last.args, wantArgs) {
		t.Fatalf("args: want %v got %v", wantArgs, r.last.args)
	}
}

func TestRemoveWorktree(t *testing.T) {
	r := &fakeRunner{}
	g := &GitObserver{Repo: "/repo", Runner: r}
	if err := g.RemoveWorktree(context.Background(), "/repo/.worktrees/x"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	wantArgs := []string{"worktree", "remove", "/repo/.worktrees/x"}
	if !reflect.DeepEqual(r.last.args, wantArgs) {
		t.Fatalf("args: want %v got %v", wantArgs, r.last.args)
	}
}
