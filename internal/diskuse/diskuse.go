// Package diskuse: тяжёлые папки зависимостей и сборок под корнем проектов.
package diskuse

import (
	"cmp"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// Kinds: имена папок, которые можно пересоздать установкой или сборкой.
var Kinds = []string{"node_modules", "vendor", ".venv", "venv", "target", "dist", "build", ".next", ".nuxt"}

// Find ищет папки Kinds под root не глубже depth; внутрь найденных и в .git не заходит.
func Find(root string, depth int) []string {
	want := map[string]bool{}
	for _, k := range Kinds {
		want[k] = true
	}
	root = filepath.Clean(root)
	base := strings.Count(root, string(filepath.Separator))
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if p == root {
			return nil
		}
		name := d.Name()
		if want[name] {
			out = append(out, p)
			return fs.SkipDir
		}
		if name == ".git" || name == "archive" || (strings.HasPrefix(name, ".") && name != ".venv") {
			return fs.SkipDir
		}
		if strings.Count(p, string(filepath.Separator))-base >= depth {
			return fs.SkipDir
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// Item: найденная папка и её размер в байтах.
type Item struct {
	Path string
	Kind string
	Size int64
}

// Summary: самые большие папки, итоги по видам и общий.
type Summary struct {
	Top    []Item
	ByKind map[string]int64
	Total  int64
}

// Summarize: top самых больших папок от minSize и итоги по видам по всем.
func Summarize(items []Item, top int, minSize int64) Summary {
	s := Summary{ByKind: map[string]int64{}}
	sorted := slices.Clone(items)
	bySize(sorted)
	for _, it := range sorted {
		s.ByKind[it.Kind] += it.Size
		s.Total += it.Size
		if len(s.Top) < top && it.Size >= minSize {
			s.Top = append(s.Top, it)
		}
	}
	return s
}

// Stale: папки, которых не касались дольше days дней, не меньше minSize, крупные первыми.
// touched - когда последний раз трогали проект, которому принадлежит папка; без отметки папку
// не предлагаем: лучше промолчать, чем удалить зависимости живого проекта.
func Stale(items []Item, touched map[string]time.Time, now time.Time, days int, minSize int64) []Item {
	limit := now.AddDate(0, 0, -days)
	var out []Item
	for _, it := range items {
		t, ok := touched[it.Path]
		if !ok || t.IsZero() || t.After(limit) || it.Size < minSize {
			continue
		}
		out = append(out, it)
	}
	bySize(out)
	return out
}

// bySize: крупные первыми, при равном размере по пути - чтобы список не прыгал между запусками.
func bySize(items []Item) {
	slices.SortStableFunc(items, func(a, b Item) int { return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(a.Path, b.Path)) })
}
