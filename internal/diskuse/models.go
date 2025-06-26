package diskuse

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Model: модель из кеша Hugging Face.
type Model struct {
	Name     string // org/name
	Path     string
	Size     int64
	LastUsed time.Time
}

// ModelName: models--mlx-community--Qwen3-8B-4bit -> mlx-community/Qwen3-8B-4bit.
func ModelName(dir string) (string, bool) {
	rest, ok := strings.CutPrefix(filepath.Base(dir), "models--")
	if !ok {
		return "", false
	}
	return strings.Replace(rest, "--", "/", 1), true
}

// HFModels: модели в hub; размер и время последнего использования заполняет вызывающий.
func HFModels(hub string) []Model {
	dirs, _ := filepath.Glob(filepath.Join(hub, "models--*"))
	var out []Model
	for _, d := range dirs {
		if name, ok := ModelName(d); ok {
			out = append(out, Model{Name: name, Path: d})
		}
	}
	return out
}

// LastUsed: когда к модели обращались последний раз - самое позднее время доступа к её файлам.
// Время скачивания тут не годится: модель могли скачать год назад и запускать каждый день.
func LastUsed(dir string) time.Time {
	var last time.Time
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		st, err := os.Stat(p) // идём по ссылкам snapshots -> blobs
		if err != nil {
			return nil
		}
		if t := accessTime(st); t.After(last) {
			last = t
		}
		return nil
	})
	return last
}

// SortModels: тяжёлые и давно не тронутые первыми - их выгоднее всего удалить.
func SortModels(list []Model) {
	sort.Slice(list, func(i, j int) bool { return list[i].Size > list[j].Size })
}

// ModelSize: размер модели по настоящим файлам. Hugging Face хранит данные по цепочке ссылок
// (snapshots -> blobs модели -> общий hub/blobs), и du по папке модели видит одни ссылки в килобайтах.
// Каждый файл считается один раз: на него ведут и snapshots, и blobs.
func ModelSize(dir string) int64 {
	seen := map[string]bool{}
	var total int64
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		real, err := filepath.EvalSymlinks(p)
		if err != nil || seen[real] {
			return nil
		}
		seen[real] = true
		if st, err := os.Stat(real); err == nil {
			total += st.Size()
		}
		return nil
	})
	return total
}
