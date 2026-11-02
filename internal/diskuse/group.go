package diskuse

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/paths"
)

// copyMin: папки меньше не ищутся в копиях - экономия на них не стоит внимания.
const copyMin = 1 << 30

// copyInside: внутри таких мест одинаковые папки - не копии, а устройство: пакеты в venv, части программы,
// уже выброшенное.
var copyInside = []string{"/node_modules/", "/.venv/", "/venv/", "/vendor/", ".app/", "/.Trash/"}

// commonNames: имена, которые повторяются сами по себе; копией их не считаем.
var commonNames = map[string]bool{"data": true, "models": true, "cache": true, "Cache": true, "Caches": true,
	"build": true, "dist": true, "files": true, "images": true, "blobs": true, "snapshots": true, "lib": true,
	"bin": true, "Application Support": true, "Library": true, "Documents": true, "Data": true, "Containers": true}

// Group: строки для глаз. Зависимости проектов - одной строкой, простые папки "Твоих файлов" - по
// проектам и папкам третьего уровня, плюс копии одной и той же папки в разных местах.
func Group(rows []Row, tree Tree, home string) []Row {
	var out, deps []Row
	byKey := map[string][]Row{}
	var keys []string
	for _, r := range rows {
		switch {
		case r.deps:
			deps = append(deps, r)
		case r.plain && r.Kind == Data && r.Path != "":
			k := groupKey(r.Path, home)
			if _, ok := byKey[k]; !ok {
				keys = append(keys, k)
			}
			byKey[k] = append(byKey[k], r)
		default:
			out = append(out, r)
		}
	}
	out = append(out, depsRow(deps)...)
	for _, k := range keys {
		out = append(out, folderRow(k, byKey[k], home))
	}
	out = append(out, Copies(tree, home)...)
	slices.SortStableFunc(out, func(a, b Row) int { return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(a.Name, b.Name)) })
	return out
}

// depsRow: node_modules, vendor, venv по всем проектам - одна строка; по одной их показывает поиск.
func depsRow(deps []Row) []Row {
	if len(deps) < 2 {
		return deps
	}
	r := Row{Name: "зависимости проектов", Hint: "пересоздаются установкой; заброшенные - tim disk --clean",
		Kind: Free, Query: "зависимости"}
	for _, d := range deps {
		r.Size += d.Size
		r.Items = append(r.Items, Spot{Path: d.Path, Size: d.Size, Name: strings.TrimPrefix(d.Name, "зависимости ")})
	}
	return []Row{r}
}

// groupKey: к чему отнести простую папку - к проекту (git-корню), иначе к папке третьего уровня от дома:
// ~/Dev/data/assets, ~/Library/Application Support/программа.
func groupKey(p, home string) string {
	if repo := repoRoot(p); repo != "" && repo != home && strings.HasPrefix(repo, home+"/") {
		// Клон в обёртке (shop/app) - один проект с обёрткой: файлы рядом с клоном туда же.
		for plainDirs[filepath.Base(repo)] && filepath.Dir(repo) != home {
			repo = filepath.Dir(repo)
		}
		return repo
	}
	base := "/"
	if strings.HasPrefix(p, home+"/") {
		base = home
	}
	rel, _ := filepath.Rel(base, p)
	parts := strings.Split(rel, "/")
	depth := 3
	// Скрытая папка в доме - данные одной программы (~/.gradle, ~/.pyenv); кроме общих .cache, .local, .config.
	if base == home && strings.HasPrefix(parts[0], ".") {
		depth = map[string]int{".cache": 2, ".config": 2, ".local": 3}[parts[0]]
		depth = max(depth, 1)
	}
	if len(parts) <= depth {
		return p
	}
	return filepath.Join(append([]string{base}, parts[:depth]...)...)
}

// folderRow: папки одного проекта или места - строкой с разбивкой; одна папка - как была.
func folderRow(key string, list []Row, home string) Row {
	if len(list) == 1 && list[0].Path == key {
		return list[0]
	}
	r := Row{Name: shortPath(key, home), Path: key, Kind: Data}
	for _, it := range list {
		r.Size += it.Size
		name, _ := filepath.Rel(key, it.Path)
		if strings.HasPrefix(it.Name, "прочее") {
			name = "прочее в " + name
			if name == "прочее в ." {
				name = "прочее"
			}
		}
		r.Items = append(r.Items, Spot{Path: it.Path, Size: it.Size, Name: name})
	}
	return r
}

// shortPath: две последние папки пути от дома - "data/assets", "Application Support/программа".
func shortPath(p, home string) string {
	rel, err := filepath.Rel(home, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = strings.TrimPrefix(p, "/")
	}
	parts := strings.Split(rel, "/")
	return strings.Join(parts[max(0, len(parts)-2):], "/")
}

// Copies: одна и та же папка в нескольких местах - одно имя (у кэша Hugging Face - имя модели) и размер в
// пределах 2%, не вложенные друг в друга. Сумма строки - сколько вернётся, если оставить одну копию.
func Copies(tree Tree, home string) []Row {
	byName := map[string][]Spot{}
	for p, size := range tree {
		if size < copyMin {
			continue
		}
		name := filepath.Base(p)
		if m, ok := ModelName(p); ok {
			name = m[strings.LastIndex(m, "/")+1:]
		}
		if commonNames[name] || plainDirs[name] || deps[name] || strings.HasPrefix(name, ".") ||
			slices.ContainsFunc(copyInside, func(s string) bool { return strings.Contains(p+"/", s) }) {
			continue
		}
		byName[name] = append(byName[name], Spot{Path: p, Size: size})
	}
	type set struct {
		name  string
		spots []Spot
	}
	var sets []set
	for name, list := range byName {
		for _, same := range sameSize(outermost(list)) {
			for _, twins := range sameFiles(same) {
				sets = append(sets, set{name, twins})
			}
		}
	}
	// Крупные первыми: копия целой папки важнее совпадений внутри неё.
	slices.SortFunc(sets, func(a, b set) int {
		return cmp.Or(cmp.Compare(b.spots[0].Size, a.spots[0].Size), cmp.Compare(a.name, b.name))
	})
	var out []Row
	var taken []string
	for _, st := range sets {
		if slices.ContainsFunc(st.spots, func(s Spot) bool { return under(s.Path, taken) }) {
			continue
		}
		n := strconv.Itoa(len(st.spots))
		r := Row{Name: "копии " + st.name + " x" + n, Kind: Data, Query: st.name, Hint: "одна папка в " + n + " местах: оставь одну", Copy: true}
		var biggest int64
		for _, s := range st.spots {
			r.Size += s.Size
			biggest = max(biggest, s.Size)
			r.Items = append(r.Items, Spot{Path: s.Path, Size: s.Size, Name: paths.Show(filepath.Dir(s.Path))})
			taken = append(taken, s.Path)
		}
		r.Size -= biggest
		out = append(out, cloned(r, st.spots, n))
	}
	return out
}

// cloned: копии, сделанные клонированием APFS (cp -c, копия в Finder), делят блоки - на диске они один раз,
// и удаление лишней не вернёт ничего. Строка тогда объясняет, а не советует.
func cloned(r Row, spots []Spot, n string) Row {
	privs := par.Map(spots, len(spots), func(s Spot) int64 {
		v, ok := PrivateSize(s.Path)
		if !ok {
			return -1
		}
		return v
	})
	var sum, most int64
	for _, v := range privs {
		if v < 0 {
			return r
		}
		sum += v
		most = max(most, v)
	}
	back := sum - most
	switch {
	case back < r.Size/10:
		r.Name = "клоны " + strings.TrimPrefix(r.Name, "копии ")
		r.Hint = "клоны APFS: в списке посчитаны все " + n + ", а на диске место занято один раз; вернётся, только если убрать все"
		r.Size -= back
		r.Copy, r.Clone = false, true
	case back < r.Size*9/10:
		r.Hint = "часть блоков у копий общая (клоны APFS): оставив одну, вернёшь столько"
		r.Size = back
	}
	return r
}

// outermost: без папок, лежащих внутри другой папки того же списка.
func outermost(list []Spot) []Spot {
	var out []Spot
	for _, s := range list {
		if !slices.ContainsFunc(list, func(o Spot) bool { return o.Path != s.Path && strings.HasPrefix(s.Path, o.Path+"/") }) {
			out = append(out, s)
		}
	}
	return out
}

// sameFiles: из папок одного размера - те, у кого совпадают первые файлы: путь и размер. Две сборки одной
// программы весят почти одинаково, но это не копии.
func sameFiles(list []Spot) [][]Spot {
	if len(list) < 2 {
		return nil
	}
	byPrint := map[string][]Spot{}
	var order []string
	for _, s := range list {
		fp := fingerprint(s.Path)
		if _, ok := byPrint[fp]; !ok {
			order = append(order, fp)
		}
		byPrint[fp] = append(byPrint[fp], s)
	}
	var out [][]Spot
	for _, fp := range order {
		if len(byPrint[fp]) > 1 && fp != "" {
			out = append(out, byPrint[fp])
		}
	}
	return out
}

// fingerprint: первые 300 файлов от мегабайта по порядку обхода - путь внутри папки и размер. Мелочь и
// скрытое не в счёт: у двух скачиваний одной модели разные служебные заметки, а сама модель та же.
func fingerprint(dir string) string {
	h := sha256.New()
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case strings.HasPrefix(d.Name(), ".") && p != dir:
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case d.IsDir():
			return nil
		case n >= 300:
			return filepath.SkipAll
		}
		info, err := d.Info()
		if err != nil || info.Size() < 1<<20 {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		_, _ = fmt.Fprintf(h, "%s %d\n", rel, info.Size())
		n++
		return nil
	})
	if n == 0 {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// sameSize: папки, чьи размеры отличаются не больше чем на 2%, - по наборам.
func sameSize(list []Spot) [][]Spot {
	slices.SortFunc(list, func(a, b Spot) int { return cmp.Compare(b.Size, a.Size) })
	var sets [][]Spot
	for _, s := range list {
		n := len(sets)
		if n > 0 && math.Abs(float64(sets[n-1][0].Size-s.Size)) <= float64(sets[n-1][0].Size)*0.02 {
			sets[n-1] = append(sets[n-1], s)
			continue
		}
		sets = append(sets, []Spot{s})
	}
	return sets
}

func under(p string, roots []string) bool {
	return slices.ContainsFunc(roots, func(r string) bool { return p == r || strings.HasPrefix(p, r+"/") })
}
