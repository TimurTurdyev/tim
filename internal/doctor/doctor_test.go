package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func bin(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPathIssues(t *testing.T) {
	root := t.TempDir()
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	_ = os.MkdirAll(a, 0o755)
	_ = os.MkdirAll(b, 0o755)
	dups, missing := PathIssues([]string{a, b, a + "/", filepath.Join(root, "gone"), ""})
	if len(dups) != 1 || len(missing) != 1 || filepath.Base(missing[0]) != "gone" {
		t.Errorf("dups %v missing %v", dups, missing)
	}
}

func TestShadowed(t *testing.T) {
	root := t.TempDir()
	first := bin(t, filepath.Join(root, "shims"), "python3")
	bin(t, filepath.Join(root, "brew"), "python3")
	bin(t, filepath.Join(root, "brew"), "go")
	link := filepath.Join(root, "link")
	_ = os.MkdirAll(link, 0o755)
	if err := os.Symlink(filepath.Join(root, "brew", "go"), filepath.Join(link, "go")); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "brew", "node"), []byte("x"), 0o644)
	bin(t, filepath.Join(root, "shims"), "node")
	got := Shadowed([]string{filepath.Join(root, "shims"), filepath.Join(root, "brew"), link}, []string{"python3", "go", "node", "php"})
	if len(got["python3"]) != 2 || got["python3"][0] != first {
		t.Errorf("python3 %v", got["python3"])
	}
	if _, ok := got["go"]; ok {
		t.Error("symlink to the same file is not shadowing")
	}
	if _, ok := got["node"]; ok {
		t.Error("non-executable file is not a program")
	}
}

func TestManagersAndConflicts(t *testing.T) {
	dirs := []string{
		"/Users/dev/.pyenv/shims",
		"/Users/dev/.local/share/mise/installs/ruby/3.3.6/bin",
		"/Users/dev/.local/share/mise/installs/python/3.12.1/bin",
		"/opt/homebrew/bin",
	}
	ms := Managers(dirs, map[string]string{"NVM_DIR": "/Users/dev/.nvm"})
	names := ""
	for _, m := range ms {
		names += m.Name + " "
	}
	if names != "mise nvm pyenv " {
		t.Fatalf("managers %q %+v", names, ms)
	}
	c := Conflicts(ms)
	if len(c) != 1 || len(c["python"]) != 2 {
		t.Errorf("conflicts %v", c)
	}
}
