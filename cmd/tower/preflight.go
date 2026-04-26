package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func preflightDir(target, repo string) error {
	info, err := os.Stat(target)
	if err == nil {
		if !info.IsDir() {
			return fmt.Errorf("%s exists but is not a directory", target)
		}
		return nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("stat %s: %w", target, err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "directory %s does not exist", target)
	if cands := candidateDirs(repo); len(cands) > 0 {
		fmt.Fprintf(&b, "\n\ndirs in %s containing markdown files:", repo)
		for _, c := range cands {
			suffix := "files"
			if c.count == 1 {
				suffix = "file"
			}
			fmt.Fprintf(&b, "\n  %s  (%d .md %s)", c.name, c.count, suffix)
		}
		b.WriteString("\n\nuse: tower discover -d <dir>")
	}
	return errors.New(b.String())
}

type candDir struct {
	name  string
	count int
}

func candidateDirs(repo string) []candDir {
	entries, err := os.ReadDir(repo)
	if err != nil {
		return nil
	}
	out := make([]candDir, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if e.Name() == "node_modules" || e.Name() == "vendor" {
			continue
		}
		n := countMD(filepath.Join(repo, e.Name()))
		if n > 0 {
			out = append(out, candDir{name: e.Name(), count: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].count != out[j].count {
			return out[i].count > out[j].count
		}
		return out[i].name < out[j].name
	})
	if len(out) > 5 {
		out = out[:5]
	}
	return out
}

func countMD(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}
