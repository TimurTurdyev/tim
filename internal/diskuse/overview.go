package diskuse

import (
	"cmp"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// Tree: размер папок в байтах по абсолютному пути.
type Tree map[string]int64

// Measure: размеры папок под root до глубины depth. Один du на всё дерево идёт минуту и больше,
// поэтому дерево режется по второму уровню и меряется параллельно; файлы первых двух уровней
// считаются сами. На другие диски (-x) и в skip не заходит. progress получает сделано и всего.
func Measure(root string, depth int, skip map[string]bool, progress func(done, total int)) Tree {
	t := Tree{}
	rootDev, _ := device(root)
	var tasks []string
	loose := map[string]int64{}
	parents := map[string][]string{}
	level1, _ := os.ReadDir(root)
	for _, e1 := range level1 {
		p1 := filepath.Join(root, e1.Name())
		if skip[p1] {
			continue
		}
		if !e1.IsDir() || e1.Type()&fs.ModeSymlink != 0 {
			loose[root] += blocks(e1)
			continue
		}
		if dev, ok := device(p1); ok && dev != rootDev {
			continue
		}
		parents[root] = append(parents[root], p1)
		level2, err := os.ReadDir(p1)
		if err != nil {
			continue
		}
		for _, e2 := range level2 {
			p2 := filepath.Join(p1, e2.Name())
			if !e2.IsDir() || e2.Type()&fs.ModeSymlink != 0 {
				loose[p1] += blocks(e2)
				continue
			}
			// du -x остаётся на диске той папки, с которой начал: точку монтирования второго
			// уровня (/mnt/data, /Volumes/USB) надо отсеять здесь, иначе чужой диск попадёт в замер.
			if dev, ok := device(p2); skip[p2] || ok && dev != rootDev {
				continue
			}
			tasks = append(tasks, p2)
			parents[p1] = append(parents[p1], p2)
		}
	}
	var mu sync.Mutex
	g := par.NewGroup(par.Limit)
	done := 0
	for _, p := range tasks {
		g.Go(func() {
			// du без прав на часть подпапок пишет ошибки, но итоги печатает - их и берём.
			out, _ := runner.OutputWithin(0, "du", "-xk", "-d", strconv.Itoa(max(depth-2, 0)), p)
			got := ParseDuAbs(out)
			mu.Lock()
			for k, v := range got {
				t[k] = v
			}
			done++
			if progress != nil {
				progress(done, len(tasks))
			}
			mu.Unlock()
		})
	}
	g.Wait()
	for _, p1 := range parents[root] {
		t[p1] = loose[p1]
		for _, p2 := range parents[p1] {
			t[p1] += t[p2]
		}
	}
	t[root] = loose[root]
	for _, p1 := range parents[root] {
		t[root] += t[p1]
	}
	return t
}

// blocks: место на диске, а не длина файла - как считают du и df.
func blocks(e fs.DirEntry) int64 {
	info, err := e.Info()
	if err != nil {
		return 0
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int64(st.Blocks) * 512
	}
	return info.Size()
}

func device(p string) (uint64, bool) {
	var st syscall.Stat_t
	if syscall.Lstat(p, &st) != nil {
		return 0, false
	}
	return uint64(st.Dev), true
}

// SizeOf: место, которое папка занимает на диске, по du -sk; без прав на часть подпапок - сколько видно.
func SizeOf(path string) int64 {
	out, _ := runner.OutputWithin(0, "du", "-sk", path)
	kb, _, _ := strings.Cut(out, "\t")
	n, _ := strconv.ParseInt(strings.TrimSpace(kb), 10, 64)
	return n * 1024
}

// ParseDuAbs: вывод du -k -> размер в байтах по пути как есть.
func ParseDuAbs(out string) Tree {
	t := Tree{}
	for _, line := range strings.Split(out, "\n") {
		kb, path, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(kb), 10, 64); err == nil {
			t[path] = n * 1024
		}
	}
	return t
}

// Prune: только папки не меньше minSize - для кэша обзора хватает их.
func (t Tree) Prune(minSize int64) Tree {
	out := Tree{}
	for k, v := range t {
		if v >= minSize {
			out[k] = v
		}
	}
	return out
}

// Spot: строка обзора - папка, где лежит много. Rest - то, что лежит в самой папке помимо
// показанных отдельно подпапок.
type Spot struct {
	Path string
	Size int64
	Rest bool
}

// Spots спускается от root туда, где место действительно лежит: пока у папки есть подпапки
// не меньше minSize, показываются они, а не она. Так "Library 271 GB" превращается в конкретную игру.
// stop - папки, внутрь которых спускаться незачем (игра, виртуалка, node_modules).
func Spots(t Tree, root string, minSize int64, stop func(string) bool) []Spot {
	kids := map[string][]string{}
	for p := range t {
		if p != root {
			kids[filepath.Dir(p)] = append(kids[filepath.Dir(p)], p)
		}
	}
	var walk func(p string) []Spot
	walk = func(p string) []Spot {
		var big []string
		for _, k := range kids[p] {
			if t[k] >= minSize {
				big = append(big, k)
			}
		}
		if len(big) == 0 || (p != root && stop != nil && stop(p)) {
			return []Spot{{Path: p, Size: t[p]}}
		}
		var out []Spot
		var sum int64
		for _, k := range big {
			out = append(out, walk(k)...)
			sum += t[k]
		}
		if rest := t[p] - sum; rest >= minSize {
			out = append(out, Spot{Path: p, Size: rest, Rest: true})
		}
		return out
	}
	out := walk(root)
	slices.SortStableFunc(out, func(a, b Spot) int { return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(a.Path, b.Path)) })
	return out
}
