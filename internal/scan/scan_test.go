package scan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkGit(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func rel(root string, list []string) []string {
	out := make([]string, len(list))
	for i, p := range list {
		r, _ := filepath.Rel(root, p)
		out[i] = filepath.ToSlash(r)
	}
	return out
}

func TestClones(t *testing.T) {
	root := t.TempDir()
	mkGit(t, root,
		"projects/shop/app",
		"projects/blog",
		"projects/blog/vendor/lib",
		"clients/acme/site/app",
		"work/node_modules/pkg",
		".hidden/repo",
		"a/b/c/d/e/too-deep",
	)
	got := strings.Join(rel(root, mustClones(t, root, 4)), " ")
	want := "clients/acme/site/app projects/blog projects/shop/app"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func mustClones(t *testing.T, root string, depth int) []string {
	t.Helper()
	list, err := Clones(root, depth)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestNamesWrapperAndClashes(t *testing.T) {
	clones := []string{
		"/r/projects/shop/app",
		"/r/clients/shop/app",
		"/r/projects/blog",
		"/r/work/Efm",
	}
	got := Names(clones, "app", nil)
	want := []string{"projects-shop", "clients-shop", "blog", "efm"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestNamesWithoutWrapper(t *testing.T) {
	got := Names([]string{"/r/shop/app"}, "", nil)
	if got[0] != "app" {
		t.Errorf("got %q", got[0])
	}
}

func TestNamesAvoidTaken(t *testing.T) {
	got := Names([]string{"/r/clients/blog"}, "", map[string]bool{"blog": true})
	if got[0] != "clients-blog" {
		t.Errorf("got %q", got[0])
	}
}
