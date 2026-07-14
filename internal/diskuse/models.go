package diskuse

import (
	"cmp"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
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

// Stats: размер модели по настоящим файлам и когда к ней обращались последний раз - одним обходом.
// Hugging Face хранит данные по цепочке ссылок (snapshots -> blobs модели -> общий hub/blobs), и du
// по папке модели видит одни ссылки в килобайтах; каждый файл считается один раз по (устройство,
// inode). Время скачивания тут не годится: модель могли скачать год назад и запускать каждый день.
func Stats(dir string) (size int64, lastUsed time.Time) {
	seen := map[[2]uint64]bool{}
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		st, err := os.Stat(p) // идём по ссылкам snapshots -> blobs
		if err != nil {
			return nil
		}
		if t := accessTime(st); t.After(lastUsed) {
			lastUsed = t
		}
		if sys, ok := st.Sys().(*syscall.Stat_t); ok {
			id := [2]uint64{uint64(sys.Dev), uint64(sys.Ino)}
			if seen[id] {
				return nil
			}
			seen[id] = true
		}
		size += st.Size()
		return nil
	})
	return size, lastUsed
}

// SortModels: тяжёлые первыми, при равном размере - давно не тронутые: их выгоднее всего удалить.
func SortModels(list []Model) {
	slices.SortStableFunc(list, func(a, b Model) int {
		return cmp.Or(cmp.Compare(b.Size, a.Size), a.LastUsed.Compare(b.LastUsed))
	})
}
