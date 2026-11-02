package diskuse

import (
	"cmp"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// plainDirs: безликие имена папок - по ним не понять, чей это проект.
var plainDirs = map[string]bool{"app": true, "src": true, "env": true, "code": true, "www": true, "repo": true}

// Обзор диска: замер с кэшем, строки по местам, тома, Steam и Chrome.

// measured: дерево размеров и какие корни в нём измерены.
type measured struct {
	Roots []string `json:"roots"`
	Tree  Tree     `json:"tree"`
}

// Row: строка обзора - место или группа мест (игры, виртуалки) с разбивкой.
type Row struct {
	Name, Hint, Path string
	Size             int64
	Items            []Spot
	Kind             Kind
	Query            string `json:"query,omitempty"` // строка-сводка: её места находит этот поиск
	// Copy: строка копий - её место уже посчитано в строках самих папок, в сумму раздела не входит.
	// У клонов APFS Size - сколько раздел насчитал лишнего, из суммы оно вычитается.
	Copy  bool   `json:"copy,omitempty"`
	Clone bool   `json:"clone,omitempty"`
	label string // подпись без имени проекта: от неё удлиняется имя при повторе
	deps  bool   // зависимости проекта
	plain bool   // место без подписи: просто папка
}

// Counted: сколько строка добавляет к сумме раздела.
func (r Row) Counted() int64 {
	switch {
	case r.Copy:
		return 0
	case r.Clone:
		return -r.Size
	}
	return r.Size
}

// DefaultRoots: что мерить без аргумента. Домашняя папка есть всегда; на macOS ещё программы и
// общие данные, под root на Linux - весь корень, иначе за пределы дома не заглянуть.
func DefaultRoots() []string {
	home := paths.Home()
	switch {
	case runtime.GOOS == "darwin":
		return []string{home, "/Applications", "/Library", "/opt"}
	case runtime.GOOS == "linux" && os.Geteuid() == 0:
		return []string{"/"}
	}
	return []string{home}
}

// coveredBy: каждая из нужных папок лежит внутри уже измеренных - пересчитывать не надо.
func coveredBy(have, want []string) bool {
	for _, w := range want {
		if !slices.ContainsFunc(have, func(h string) bool { return w == h || strings.HasPrefix(w, strings.TrimSuffix(h, "/")+"/") }) {
			return false
		}
	}
	return true
}

func CachePath() string { return paths.State("disk-map.json") }

// Map: дерево размеров. Свежий замер домашней папки идёт десятки секунд, поэтому
// результат живёт 15 минут: сразу после краткого обзора --all не пересчитывает заново.
func Map(roots []string, fresh bool, progress func(done, total int, root string)) (Tree, time.Time) {
	if c, ok := cache.Load[measured](CachePath()); ok && !fresh && c.Fresh(15*time.Minute) && coveredBy(c.Value.Roots, roots) {
		return c.Value.Tree, c.Fetched
	}
	skip := map[string]bool{"/proc": true, "/sys": true, "/dev": true, "/run": true}
	t := Tree{}
	for _, r := range roots {
		for k, v := range Measure(r, 8, skip, func(done, total int) {
			if progress != nil {
				progress(done, total, r)
			}
		}) {
			t[k] = v
		}
	}
	pruned := t.Prune(50 << 20)
	_ = cache.Store(CachePath(), measured{Roots: roots, Tree: pruned}, 0o600)
	return pruned, time.Now()
}

// Rows: места в строки. Всё внутри группы (игры Steam, виртуалки) - одной строкой с разбивкой.
func Rows(spots []Spot, home string) []Row {
	var rows []Row
	groups := map[string]int{}
	for _, s := range spots {
		l, group, ok := LabelOf(s.Path, home)
		if ok && group != "" {
			i, seen := groups[group]
			if !seen {
				i = len(rows)
				groups[group] = i
				rows = append(rows, Row{Name: l.Name, Hint: l.Hint, Path: group, Kind: l.Kind})
			}
			rows[i].Size += s.Size
			rows[i].Items = append(rows[i].Items, s)
			continue
		}
		r := Row{Path: s.Path, Size: s.Size, Kind: Data}
		switch {
		case s.Rest:
			r.Name = "прочее в " + filepath.Base(s.Path)
		case ok:
			r.Name, r.Hint, r.Kind = l.Name, l.Hint, l.Kind
		}
		// Имя проекта - только у мест проекта: у Homebrew или Корзины его быть не должно.
		if (!ok || l.Owned) && !s.Rest {
			r.label = r.Name
			r.Name = placeName(r.Name, s.Path, ownerName(s.Path, home, 1))
		}
		r.deps, r.plain = ok && l.Deps, !ok
		rows = append(rows, r)
	}
	distinct(rows, home)
	slices.SortStableFunc(rows, func(a, b Row) int { return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(a.Name, b.Name)) })
	return rows
}

// distinct: одинаковые имена мест проектов удлиняются на папку выше, пока не станут разными - шесть
// строк "зависимости .venv: env" не отличить.
func distinct(rows []Row, home string) {
	for levels := 2; levels <= 4; levels++ {
		seen := map[string]int{}
		for _, r := range rows {
			seen[r.Name]++
		}
		changed := false
		for i, r := range rows {
			if seen[r.Name] > 1 && r.label != "" {
				if name := placeName(r.label, r.Path, ownerName(r.Path, home, levels)); name != r.Name {
					rows[i].Name, changed = name, true
				}
			}
		}
		if !changed {
			return
		}
	}
}

// ownerName: чьё место - проект, в котором оно лежит, и путь до места внутри проекта: "simple-talk/tools/bench".
// Проект - git-корень внутри домашней папки; безликое app, src, env заменяется папкой выше. levels -
// сколько папок от проекта взять в имя. Вне репозитория - папка над местом.
func ownerName(path, home string, levels int) string {
	base, inRepo := filepath.Dir(path), false
	if repo := repoRoot(path); repo != "" && repo != home && strings.HasPrefix(repo, home+"/") {
		base, inRepo = repo, true
	}
	dir := base
	for plainDirs[filepath.Base(dir)] && filepath.Dir(dir) != home && filepath.Dir(dir) != dir {
		dir = filepath.Dir(dir)
	}
	rel, err := filepath.Rel(home, dir)
	if err != nil || strings.HasPrefix(rel, "..") {
		rel = dir
	}
	parts := strings.Split(strings.Trim(rel, "/"), "/")
	name := strings.Join(parts[max(0, len(parts)-levels):], "/")
	// В репозитории безликий корень (app) в имя не идёт; вне репозитория он и есть отличие: data/env.
	from := dir
	if inRepo {
		from = base
	}
	if inner, err := filepath.Rel(from, filepath.Dir(path)); err == nil && inner != "." && !strings.HasPrefix(inner, "..") {
		name += "/" + inner
	}
	return name
}

// ProjectName: чьё место для подписи вне обзора - как в строках обзора.
func ProjectName(path, home string) string { return ownerName(path, home, 1) }

// placeName: подпись с тем, чьё это: "файлы Laravel: shop-admin", "бэкап example.com".
// У безымянной папки - репозиторий, в котором она лежит, и путь внутри него.
func placeName(name, path, owner string) string {
	switch {
	case name == "бэкапы":
		if _, after, ok := strings.Cut(path, "/backups/"); ok {
			return "бэкап " + strings.Split(after, "/")[0]
		}
	case name != "" && owner != "":
		return name + ": " + owner
	case name == "":
		if repo := repoRoot(path); repo != "" && repo != path {
			r, _ := filepath.Rel(repo, path)
			return filepath.Base(repo) + ": " + r
		}
		return filepath.Base(filepath.Dir(path)) + "/" + filepath.Base(path)
	}
	return name
}

// repoRoot: ближайшая папка с .git над path.
func repoRoot(path string) string {
	for d := filepath.Dir(path); d != "/" && d != "."; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
	}
	return ""
}

// PlayedAgo: когда последний раз запускали игру; stale - давно или никогда, это первые кандидаты.
func PlayedAgo(t time.Time) (text string, stale bool) {
	if t.IsZero() {
		return "не запускали", true
	}
	return "играли " + t.Format("02.01.2006"), time.Since(t) > 180*24*time.Hour
}

// VolumeRows: другие тома того же контейнера APFS. Их место вычитается из свободного,
// но в папках его не найти: служебные тома macOS и тома, которые завёл сам пользователь.
func VolumeRows() []Row {
	if runtime.GOOS != "darwin" {
		return nil
	}
	out, _ := runner.Output("diskutil", "apfs", "list")
	sys := Row{Name: "macOS и служебные тома", Kind: System}
	var rows []Row
	for _, v := range ParseAPFS(out, "/System/Volumes/Data") {
		switch v.Role {
		case "Data":
		case "System", "Preboot", "Recovery", "VM", "Update", "xART", "Hardware":
			sys.Size += v.Size
		default:
			rows = append(rows, Row{Name: "том " + v.Name, Size: v.Size, Kind: Data, Hint: "отдельный том на этом же диске; не нужен - удалить в Дисковой утилите"})
		}
	}
	if sys.Size > 0 {
		rows = append(rows, sys)
	}
	return rows
}

// ChromeClones: сколько копий себя Chrome оставил во временной папке системы.
func ChromeClones() int {
	dirs, _ := filepath.Glob("/private/var/folders/*/*/X/com.google.Chrome.code_sign_clone/*")
	return len(dirs)
}

func SteamInstalled() bool {
	if runtime.GOOS != "darwin" {
		return runner.Has("steam")
	}
	for _, p := range []string{"/Applications/Steam.app", filepath.Join(paths.Home(), "Applications", "Steam.app")} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// MoveInTree: папка уехала в Корзину - её размер уходит от прежних родителей и приходит к Корзине.
// На диске место не освободилось, пока Корзина не очищена, и обозреватель должен это показывать.
func (tree Tree) MoveInTree(path, trash string) {
	size := tree[path]
	for k := range tree {
		if k == path || strings.HasPrefix(k, path+"/") {
			delete(tree, k)
		}
	}
	for d := filepath.Dir(path); d != "/" && d != "."; d = filepath.Dir(d) {
		if _, ok := tree[d]; ok {
			tree[d] -= size
		}
	}
	tree[trash] += size
	for d := filepath.Dir(trash); d != "/" && d != "."; d = filepath.Dir(d) {
		if _, ok := tree[d]; ok {
			tree[d] += size
		}
	}
}

// ParseSize: 50M, 1G, 500K или байты.
func ParseSize(s string) (int64, error) {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := int64(1)
	for suf, m := range map[string]int64{"K": 1 << 10, "M": 1 << 20, "G": 1 << 30} {
		if strings.HasSuffix(s, suf) {
			mult, s = m, strings.TrimSuffix(s, suf)
		}
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, ui.Errorf("размер вида 50M: %s", s)
	}
	return int64(n * float64(mult)), nil
}
