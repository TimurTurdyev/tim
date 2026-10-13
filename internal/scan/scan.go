// Package scan: поиск git-клонов под корнем проектов и выбор для них имён в реестре.
package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// plainNames: безликие имена клонов - проект называется по папке выше.
var plainNames = map[string]bool{"app": true, "src": true, "code": true, "repo": true}

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

// Names даёт каждому клону уникальное имя: базовое имя папки, а если оно равно wrapper или безлико
// (app, src) - имя родителя: одна обёртка shop/app ещё не даёт угадать wrapper по повторам.
// При совпадениях к имени слева добавляются родительские папки: у каждого клона - самый короткий
// хвост пути, которого нет у других клонов и среди занятых имён.
func Names(clones []string, wrapper string, taken map[string]bool) []string {
	parts := make([][]string, len(clones))
	for i, c := range clones {
		segs := strings.Split(filepath.ToSlash(filepath.Clean(c)), "/")
		if last := segs[len(segs)-1]; len(segs) > 1 && (wrapper != "" && last == wrapper || plainNames[last]) {
			segs = segs[:len(segs)-1]
		}
		parts[i] = segs
	}
	// Сколько клонов имеют такой же хвост из n папок: хвост уникален, когда он один.
	tails := map[string]int{}
	for _, segs := range parts {
		for n := 1; n <= len(segs); n++ {
			tails[join(segs, n)]++
		}
	}
	names := make([]string, len(clones))
	for i, segs := range parts {
		n := 1
		for n < len(segs) && (tails[join(segs, n)] > 1 || taken[join(segs, n)]) {
			n++
		}
		names[i] = join(segs, n)
	}
	return names
}

func join(segs []string, n int) string {
	if n > len(segs) {
		n = len(segs)
	}
	return strings.ToLower(strings.Join(segs[len(segs)-n:], "-"))
}
