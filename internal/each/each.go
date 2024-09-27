// Package each: папки проектов с файлом-маркером и версия из него.
package each

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// JSONVersion: значение ключа key из JSON-файла; пусто, если файла или ключа нет.
func JSONVersion(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

// Older: версия a младше b; сравниваются числа через точку, хвост вида -beta отбрасывается.
func Older(a, b string) bool {
	pa, pb := nums(a), nums(b)
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			return x < y
		}
	}
	return false
}

func nums(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexFunc(v, func(r rune) bool { return r >= '0' && r <= '9' }); i > 0 {
		v = v[i:]
	}
	v, _, _ = strings.Cut(v, "-")
	if f := strings.Fields(v); len(f) > 0 {
		v = f[0]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		out = append(out, n)
	}
	return out
}

// Locate оставляет из кандидатов папки, где есть marker, без повторов, по порядку.
func Locate(candidates []string, marker string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range candidates {
		d = filepath.Clean(d)
		if seen[d] {
			continue
		}
		seen[d] = true
		if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
			out = append(out, d)
		}
	}
	return out
}

var skip = map[string]bool{"node_modules": true, "vendor": true, ".venv": true, "venv": true, "archive": true}

// Scan ищет marker под root не глубже depth.
func Scan(root, marker string, depth int) []string {
	root = filepath.Clean(root)
	base := strings.Count(root, string(filepath.Separator))
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if p != root && (skip[d.Name()] || strings.HasPrefix(d.Name(), ".")) {
			return fs.SkipDir
		}
		if _, err := os.Stat(filepath.Join(p, marker)); err == nil {
			out = append(out, p)
		}
		if strings.Count(p, string(filepath.Separator))-base >= depth {
			return fs.SkipDir
		}
		return nil
	})
	sort.Strings(out)
	return out
}
