package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Вложенный проект побеждает родителя: из папки handbook - он, а не весь a.
func TestProjectHere(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{Dev: dir, Projects: []Project{
		{Name: "short", Path: "projects/b/app"},
		{Name: "a", Path: "projects/a/app"},
		{Name: "a-hb", Path: "projects/a/app/handbook"},
	}}
	for _, d := range []string{"projects/a/app/handbook/php", "projects/b/app"} {
		_ = os.MkdirAll(filepath.Join(dir, d), 0o755)
	}
	if p, ok := cfg.ProjectHere(filepath.Join(dir, "projects/a/app/handbook/php")); !ok || p.Name != "a-hb" {
		t.Errorf("here: want a-hb, got %v", p)
	}
	if p, ok := cfg.ProjectHere(filepath.Join(dir, "projects/a/app")); !ok || p.Name != "a" {
		t.Errorf("here: want a, got %v", p)
	}
	if _, ok := cfg.ProjectHere(os.TempDir()); ok {
		t.Error("outside must not match")
	}
}
