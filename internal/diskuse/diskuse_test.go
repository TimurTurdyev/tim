package diskuse

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFind(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{
		"shop/app/node_modules/lib/node_modules",
		"shop/app/vendor/pkg",
		"blog/.venv/lib",
		"blog/.git/objects/dist",
		"blog/.cache/build",
		"tool/target/debug",
		"a/b/c/d/e/f/node_modules",
	} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := Find(root, 5)
	var rel []string
	for _, p := range got {
		r, _ := filepath.Rel(root, p)
		rel = append(rel, filepath.ToSlash(r))
	}
	want := []string{"blog/.venv", "shop/app/node_modules", "shop/app/vendor", "tool/target"}
	if len(rel) != len(want) {
		t.Fatalf("got %v", rel)
	}
	for i := range want {
		if rel[i] != want[i] {
			t.Fatalf("got %v want %v", rel, want)
		}
	}
}

func TestSummarize(t *testing.T) {
	items := []Item{{"a", "node_modules", 300}, {"b", "vendor", 50}, {"c", "node_modules", 100}, {"d", "dist", 5}}
	s := Summarize(items, 2, 10)
	if s.Total != 455 || s.ByKind["node_modules"] != 400 || len(s.Top) != 2 || s.Top[0].Path != "a" || s.Top[1].Path != "c" {
		t.Errorf("%+v", s)
	}
	if s := Summarize(items, 10, 60); len(s.Top) != 2 {
		t.Errorf("min filter %+v", s.Top)
	}
}

func TestStale(t *testing.T) {
	now := time.Date(2025, 10, 20, 0, 0, 0, 0, time.UTC)
	items := []Item{
		{Path: "/d/old/node_modules", Size: 500 << 20},
		{Path: "/d/old/vendor", Size: 10 << 20},          // мелкая
		{Path: "/d/fresh/node_modules", Size: 900 << 20}, // трогали вчера
		{Path: "/d/unknown/dist", Size: 800 << 20},       // неизвестно когда трогали
		{Path: "/d/older/.venv", Size: 700 << 20},
	}
	touched := map[string]time.Time{
		"/d/old/node_modules":   now.AddDate(0, 0, -90),
		"/d/old/vendor":         now.AddDate(0, 0, -90),
		"/d/fresh/node_modules": now.AddDate(0, 0, -1),
		"/d/older/.venv":        now.AddDate(-1, 0, 0),
	}
	got := Stale(items, touched, now, 60, 50<<20)
	if len(got) != 2 || got[0].Path != "/d/older/.venv" || got[1].Path != "/d/old/node_modules" {
		t.Fatalf("%+v", got)
	}
}
