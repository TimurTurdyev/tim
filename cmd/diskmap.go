package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	diskAll   bool
	diskFresh bool
)

// mapRoots: что мерить без аргумента. Домашняя папка есть всегда; на macOS ещё программы и
// общие данные, под root на Linux - весь корень, иначе за пределы дома не заглянуть.
func mapRoots() []string {
	home := paths.Home()
	switch {
	case runtime.GOOS == "darwin":
		return []string{home, "/Applications", "/Library", "/opt"}
	case runtime.GOOS == "linux" && os.Geteuid() == 0:
		return []string{"/"}
	}
	return []string{home}
}

type mapCache struct {
	At    time.Time    `json:"at"`
	Roots []string     `json:"roots"`
	Tree  diskuse.Tree `json:"tree"`
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

func mapCachePath() string { return paths.State("disk-map.json") }

// measureMap: дерево размеров. Свежий замер домашней папки идёт десятки секунд, поэтому
// результат живёт 15 минут: сразу после краткого обзора --all не пересчитывает заново.
func measureMap(roots []string, fresh bool, progress func(done, total int, root string)) (diskuse.Tree, time.Time) {
	if !fresh {
		if data, err := os.ReadFile(mapCachePath()); err == nil {
			var c mapCache
			if json.Unmarshal(data, &c) == nil && coveredBy(c.Roots, roots) && time.Since(c.At) < 15*time.Minute {
				return c.Tree, c.At
			}
		}
	}
	skip := map[string]bool{"/proc": true, "/sys": true, "/dev": true, "/run": true}
	t := diskuse.Tree{}
	for _, r := range roots {
		for k, v := range diskuse.Measure(r, 8, skip, func(done, total int) {
			if progress != nil {
				progress(done, total, r)
			}
		}) {
			t[k] = v
		}
	}
	pruned := t.Prune(50 << 20)
	if data, err := json.Marshal(mapCache{At: time.Now(), Roots: roots, Tree: pruned}); err == nil {
		_ = os.MkdirAll(filepath.Dir(mapCachePath()), 0o755)
		_ = os.WriteFile(mapCachePath(), data, 0o600)
	}
	return pruned, time.Now()
}

// cliProgress: ход замера в stderr, пока команда идёт в терминале.
func cliProgress() func(done, total int, root string) {
	if !ui.Terminal() {
		return nil
	}
	return func(done, total int, root string) {
		fmt.Fprintf(os.Stderr, "\r\033[2K"+ui.Dim("считаю %s: %d из %d"), paths.Show(root), done, total)
		if done == total {
			fmt.Fprint(os.Stderr, "\r\033[2K")
		}
	}
}

type mapRow struct {
	name, hint, path string
	size             int64
	items            []diskuse.Spot
	kind             diskuse.Kind
}

func diskMap(args []string) error {
	roots := mapRoots()
	if len(args) > 0 {
		r, err := filepath.Abs(filepathExpand(args[0]))
		if err != nil {
			return err
		}
		roots = []string{r}
	}
	min := int64(1 << 30)
	if diskAll {
		min = 100 << 20
	}
	tree, at := measureMap(roots, diskFresh, cliProgress())
	home := paths.Home()
	stop := func(p string) bool {
		l, _, ok := diskuse.LabelOf(p, home)
		return ok && l.Stop
	}
	var spots []diskuse.Spot
	var measured int64
	for _, r := range roots {
		spots = append(spots, diskuse.Spots(tree, r, min, stop)...)
		measured += tree[r]
	}
	rows := groupSpots(spots, home)
	var volumes int64
	if len(args) == 0 {
		for _, r := range volumeRows() {
			volumes += r.size
			rows = append(rows, r)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].size > rows[j].size })
	}
	var unknown int64
	if d, ok := sysinfo.DataDisk(); ok && len(args) == 0 {
		pct := d.Percent()
		ui.Println(ui.Title("Диск") + "  " + bar(pct, 30) + ui.Dim("свободно ") + ui.Bytes(int64(d.Avail)) + ui.Dim(" из "+ui.Bytes(int64(d.Size))))
		unknown = int64(d.Size-d.Avail) - measured - volumes
	}
	// Кратко - по несколько самых больших в разделе; освобождаемого больше: с него и начинают.
	limit := map[diskuse.Kind]int{diskuse.Free: 8, diskuse.Apps: 3, diskuse.Data: 6, diskuse.System: 2}
	var shown []mapRow
	taken := map[diskuse.Kind]int{}
	for _, r := range rows {
		if diskAll || taken[r.kind] < limit[r.kind] {
			shown = append(shown, r)
			taken[r.kind]++
		}
	}
	var top int64
	if len(rows) > 0 {
		top = rows[0].size
	}
	sections := []struct {
		kind  diskuse.Kind
		title string
	}{
		{diskuse.Free, "Можно освободить"},
		{diskuse.Apps, "Программы"},
		{diskuse.Data, "Твои файлы - решать тебе"},
		{diskuse.System, "Система"},
	}
	for _, sec := range sections {
		var in []mapRow
		var total int64
		for _, r := range rows {
			if r.kind == sec.kind {
				total += r.size
			}
		}
		for _, r := range shown {
			if r.kind == sec.kind {
				in = append(in, r)
			}
		}
		if len(in) == 0 {
			continue
		}
		ui.Println()
		ui.Println(ui.Title(sec.title) + ui.Dim("  "+ui.Bytes(total)))
		for _, r := range in {
			printMapRow(r, top)
		}
	}
	ui.Println()
	var notes []string
	if len(shown) < len(rows) {
		notes = append(notes, "показано "+strconv.Itoa(len(shown))+" из "+strconv.Itoa(len(rows))+", все от 100 MB: --all")
	}
	if age := time.Since(at); age > time.Minute {
		notes = append(notes, "замер "+strconv.Itoa(int(age.Minutes()))+" мин назад, заново: --fresh")
	}
	if len(notes) > 0 {
		ui.Println(ui.Dim(strings.Join(notes, "  \u2022  ")))
	}
	ui.Println(ui.Dim("копать по папкам: ") + "tim disk -b" + ui.Dim("  \u2022  --system кеши  \u2022  --deps зависимости  \u2022  --leftovers хвосты программ"))
	if n := chromeClones(); n > 0 && len(args) == 0 {
		ui.Println(ui.Warn("Chrome хранит "+strconv.Itoa(n)+" "+ui.Plural(n, "копию", "копии", "копий")+" самого себя") +
			ui.Dim(" - известная ошибка. Закрой Chrome и: trash /private/var/folders/*/*/X/com.google.Chrome.code_sign_clone"))
	}
	if unknown > 1<<30 {
		ui.Println(ui.Dim("не разобрано " + ui.Bytes(unknown) + ": без прав не прочитать, снимки Time Machine, копии Chrome"))
	}
	return nil
}

func filepathExpand(p string) string {
	if p == "~" {
		return paths.Home()
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(paths.Home(), p[2:])
	}
	return p
}

// groupSpots: места в строки. Всё внутри группы (игры Steam, виртуалки) - одной строкой с разбивкой.
func groupSpots(spots []diskuse.Spot, home string) []mapRow {
	var rows []mapRow
	groups := map[string]int{}
	for _, s := range spots {
		l, group, ok := diskuse.LabelOf(s.Path, home)
		if ok && group != "" {
			i, seen := groups[group]
			if !seen {
				i = len(rows)
				groups[group] = i
				rows = append(rows, mapRow{name: l.Name, hint: l.Hint, path: group, kind: l.Kind})
			}
			rows[i].size += s.Size
			rows[i].items = append(rows[i].items, s)
			continue
		}
		r := mapRow{path: s.Path, size: s.Size, kind: diskuse.Data}
		switch {
		case s.Rest:
			r.name = "прочее в " + filepath.Base(s.Path)
		case ok:
			r.name, r.hint, r.kind = l.Name, l.Hint, l.Kind
		}
		r.name = placeName(r.name, s.Path)
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].size > rows[j].size })
	return rows
}

// placeName: подпись с тем, чьё это: "файлы Laravel: shop-admin", "бэкап example.com".
// У безымянной папки - репозиторий, в котором она лежит, и путь внутри него.
func placeName(name, path string) string {
	owner := ""
	if p, ok := cfg.ProjectHere(path); ok {
		owner = p.Name
	}
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
		if owner != "" {
			return "проект " + owner
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

// sizeBar: короткая полоска размера места относительно самого большого - глазом видно, что важно.
func sizeBar(size, top int64) string {
	const w = 8
	n := 1
	if top > 0 {
		n = max(1, int(size*w/top))
	}
	return ui.Info(strings.Repeat("\u2588", n)) + ui.Dim(strings.Repeat("\u2591", w-n))
}

func printMapRow(r mapRow, top int64) {
	name := r.name
	if name == "" {
		name = filepath.Base(r.path)
	}
	where := ""
	if r.path != "" {
		where = paths.Show(r.path)
	}
	ui.Println("  " + ui.Label(ui.Pad(ui.Bytes(r.size), 9)) + sizeBar(r.size, top) + "  " + ui.Pad(ui.Clip(name, 38), 40) + ui.Dim(where))
	const indent = "                       "
	if len(r.items) > 0 {
		var games map[string]diskuse.Game
		if strings.HasSuffix(r.path, "steamapps/common") {
			games = diskuse.SteamGames(r.path)
		}
		var parts []string
		var rest int64
		more := 0
		for _, it := range r.items {
			if it.Rest {
				rest += it.Size
				continue
			}
			if !diskAll && len(parts) >= 4 {
				rest += it.Size
				more++
				continue
			}
			label := diskuse.ItemName(it.Path)
			if g, ok := games[filepath.Base(it.Path)]; ok {
				label = g.Name + " " + ui.Bytes(it.Size) + ", " + playedAgo(g.LastPlayed)
				ui.Println(indent + label)
				parts = append(parts, "")
				continue
			}
			parts = append(parts, label+" "+ui.Bytes(it.Size))
		}
		var inline []string
		for _, p := range parts {
			if p != "" {
				inline = append(inline, p)
			}
		}
		if more > 0 {
			inline = append(inline, "ещё "+strconv.Itoa(more))
		} else if rest > 0 && len(inline) > 0 {
			inline = append(inline, "мелочь "+ui.Bytes(rest))
		}
		// Одна виртуалка или программа в группе - имя и так понятно из строки, разбивка лишняя.
		if len(inline) > 1 || (len(inline) == 1 && len(r.items) > 1) || (len(inline) == 1 && !strings.HasSuffix(inline[0], ui.Bytes(r.size))) {
			ui.Println(indent + ui.Dim(strings.Join(inline, "  \u00b7  ")))
		} else if len(inline) == 1 {
			ui.Println(indent + ui.Dim(strings.TrimSuffix(inline[0], " "+ui.Bytes(r.size))))
		}
	}
	if strings.HasSuffix(r.path, "steamapps/common") && !steamInstalled() {
		// Удаление Steam игры не трогает: они так и лежат, а удалить их через Steam уже нечем.
		lib := filepath.Dir(filepath.Dir(r.path))
		ui.Println(indent + ui.Warn("Steam удалён, а игры остались") + ui.Dim(": trash "+shellq.Join([]string{lib})))
		return
	}
	if r.hint != "" {
		ui.Println(indent + ui.Dim("\u2192 "+r.hint))
	}
}

// playedAgo: когда последний раз запускали игру; давно - предупреждением, это первые кандидаты.
func playedAgo(t time.Time) string {
	if t.IsZero() {
		return ui.Warn("не запускали")
	}
	days := int(time.Since(t).Hours() / 24)
	text := "играли " + t.Format("02.01.2006")
	if days > 180 {
		return ui.Warn(text)
	}
	return ui.Dim(text)
}

// volumeRows: другие тома того же контейнера APFS. Их место вычитается из свободного,
// но в папках его не найти: служебные тома macOS и тома, которые завёл сам пользователь.
func volumeRows() []mapRow {
	if runtime.GOOS != "darwin" {
		return nil
	}
	out, _ := runner.Output("diskutil", "apfs", "list")
	sys := mapRow{name: "macOS и служебные тома", kind: diskuse.System}
	var rows []mapRow
	for _, v := range diskuse.ParseAPFS(out, "/System/Volumes/Data") {
		switch v.Role {
		case "Data":
		case "System", "Preboot", "Recovery", "VM", "Update", "xART", "Hardware":
			sys.size += v.Size
		default:
			rows = append(rows, mapRow{name: "том " + v.Name, size: v.Size, kind: diskuse.Data, hint: "отдельный том на этом же диске; не нужен - удалить в Дисковой утилите"})
		}
	}
	if sys.size > 0 {
		rows = append(rows, sys)
	}
	return rows
}

// chromeClones: сколько копий себя Chrome оставил во временной папке системы.
func chromeClones() int {
	dirs, _ := filepath.Glob("/private/var/folders/*/*/X/com.google.Chrome.code_sign_clone/*")
	return len(dirs)
}

func steamInstalled() bool {
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
