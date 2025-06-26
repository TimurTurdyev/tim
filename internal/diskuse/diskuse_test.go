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

func TestSummarizeSystemNoDoubleCount(t *testing.T) {
	places := []Place{
		{Name: "Homebrew", Paths: []string{"Library/Caches/Homebrew"}},
		{Name: "npm", Paths: []string{".npm"}},
		{Name: "пусто", Paths: []string{"nothing"}},
	}
	sizes := map[string]int64{"Library/Caches": 10 << 30, "Library/Caches/Homebrew": 4 << 30, ".npm": 1 << 30}
	got := SummarizeSystem(places, sizes)
	if len(got) != 3 {
		t.Fatalf("%+v", got)
	}
	// Caches 10 ГБ, из них 4 - Homebrew: прочего остаётся 6, а не 10.
	if got[0].Name != "~/Library/Caches: прочее" || got[0].Size != 6<<30 || got[1].Name != "Homebrew" {
		t.Fatalf("%+v", got)
	}
}

func TestOldIDEVersions(t *testing.T) {
	old := OldIDEVersions([]string{"GoLand2024.2", "GoLand2025.2", "PhpStorm2025.2", "PhpStorm2025.3",
		"PyCharm2024.3", "Toolbox", "Daemon", "acp-agents"})
	if !old["GoLand2024.2"] || !old["PhpStorm2025.2"] || len(old) != 2 {
		t.Fatalf("%v", old)
	}
	// Единственная версия продукта не старая, даже если давняя: это могла быть текущая.
	if old["PyCharm2024.3"] {
		t.Fatal("PyCharm без более новой версии помечен старым")
	}
}

func TestModelName(t *testing.T) {
	if n, ok := ModelName("/x/hub/models--mlx-community--Qwen3-8B-4bit"); !ok || n != "mlx-community/Qwen3-8B-4bit" {
		t.Fatalf("%q %v", n, ok)
	}
	// Двойной дефис в имени самой модели не должен превращаться в слеш.
	if n, _ := ModelName("models--org--a--b"); n != "org/a--b" {
		t.Fatalf("%q", n)
	}
	if _, ok := ModelName("datasets--x--y"); ok {
		t.Fatal("наборы данных - не модели")
	}
}

func TestSummarizeTwoParents(t *testing.T) {
	places := []Place{{Name: "hf", Paths: []string{".cache/huggingface"}}, {Name: "brew", Paths: []string{"Library/Caches/Homebrew"}}}
	got := SummarizeSystem(places, map[string]int64{".cache": 40, ".cache/huggingface": 34, "Library/Caches": 10, "Library/Caches/Homebrew": 4})
	m := map[string]int64{}
	for _, x := range got {
		m[x.Name] = x.Size
	}
	if m["hf"] != 34 || m["~/.cache: прочее"] != 6 || m["~/Library/Caches: прочее"] != 6 {
		t.Fatalf("%v", m)
	}
}

func TestModelSizeFollowsSharedBlobs(t *testing.T) {
	hub := t.TempDir()
	shared := filepath.Join(hub, "blobs", "1c")
	model := filepath.Join(hub, "models--org--m")
	for _, d := range []string{shared, filepath.Join(model, "blobs"), filepath.Join(model, "snapshots", "rev")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Данные лежат в общем hub/blobs, к ним ведут две ссылки подряд.
	if err := os.WriteFile(filepath.Join(shared, "big"), make([]byte, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.Symlink("../../blobs/1c/big", filepath.Join(model, "blobs", "h1"))
	_ = os.Symlink("../../blobs/h1", filepath.Join(model, "snapshots", "rev", "model.safetensors"))
	_ = os.WriteFile(filepath.Join(model, "blobs", "small"), make([]byte, 100), 0o644)
	_ = os.Symlink("../../blobs/small", filepath.Join(model, "snapshots", "rev", "config.json"))
	if got := ModelSize(model); got != 5100 {
		t.Fatalf("размер %d, ожидали 5100: каждый файл один раз, по настоящим данным", got)
	}
}
