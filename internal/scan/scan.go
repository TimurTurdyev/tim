// Package scan: поиск git-клонов под корнем проектов и выбор для них имён в реестре.
package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var skip = map[string]bool{
	"node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"dist": true, "build": true, "archive": true,
}

// Clones возвращает пути клонов (папок с .git) не глубже depth уровней; внутрь клонов не спускается.
func Clones(root string, depth int) ([]string, error) {
	root = filepath.Clean(root)
	base := strings.Count(root, string(filepath.Separator))
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return fs.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		if p != root {
			name := d.Name()
			if skip[name] || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
		}
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			out = append(out, p)
			return fs.SkipDir
		}
		if strings.Count(p, string(filepath.Separator))-base >= depth {
			return fs.SkipDir
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// Names даёт каждому клону уникальное имя: базовое имя папки, а если оно равно wrapper - имя родителя.
// При совпадениях к имени слева добавляются родительские папки, пока имена не разойдутся.
func Names(clones []string, wrapper string, taken map[string]bool) []string {
	parts := make([][]string, len(clones))
	for i, c := range clones {
		segs := strings.Split(filepath.ToSlash(filepath.Clean(c)), "/")
		if wrapper != "" && len(segs) > 1 && segs[len(segs)-1] == wrapper {
			segs = segs[:len(segs)-1]
		}
		parts[i] = segs
	}
	width := make([]int, len(clones))
	for i := range width {
		width[i] = 1
	}
	for range 8 {
		names := make([]string, len(clones))
		count := map[string]int{}
		for i := range clones {
			names[i] = join(parts[i], width[i])
			count[names[i]]++
		}
		clash := false
		for i, n := range names {
			if (count[n] > 1 || taken[n]) && width[i] < len(parts[i]) {
				width[i]++
				clash = true
			}
		}
		if !clash {
			return names
		}
	}
	names := make([]string, len(clones))
	for i := range clones {
		names[i] = join(parts[i], width[i])
	}
	return names
}

func join(segs []string, n int) string {
	if n > len(segs) {
		n = len(segs)
	}
	return strings.ToLower(strings.Join(segs[len(segs)-n:], "-"))
}
