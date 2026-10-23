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
func measureMap(roots []string, fresh bool) (diskuse.Tree, time.Time) {
	if !fresh {
		if data, err := os.ReadFile(mapCachePath()); err == nil {
			var c mapCache
			if json.Unmarshal(data, &c) == nil && coveredBy(c.Roots, roots) && time.Since(c.At) < 15*time.Minute {
				return c.Tree, c.At
			}
		}
	}
	skip := map[string]bool{"/proc": true, "/sys": true, "/dev": true, "/run": true}
	tty := stdoutTerminal()
	t := diskuse.Tree{}
	for _, r := range roots {
		progress := func(done, total int) {
			if tty {
				fmt.Fprintf(os.Stderr, "\r\033[2K"+ui.Dim("считаю %s: %d из %d"), showPath(r), done, total)
			}
		}
		for k, v := range diskuse.Measure(r, 8, skip, progress) {
			t[k] = v
		}
	}
	if tty {
		fmt.Fprint(os.Stderr, "\r\033[2K")
	}
	pruned := t.Prune(50 << 20)
	if data, err := json.Marshal(mapCache{At: time.Now(), Roots: roots, Tree: pruned}); err == nil {
		_ = os.MkdirAll(filepath.Dir(mapCachePath()), 0o755)
		_ = os.WriteFile(mapCachePath(), data, 0o600)
	}
	return pruned, time.Now()
}

func stdoutTerminal() bool {
	st, err := os.Stderr.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// showPath: путь с ~ вместо домашней папки.
func showPath(p string) string {
	home := paths.Home()
	if r, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(r, "..") {
		if r == "." {
			return "~"
		}
		return "~/" + r
	}
	return p
}

type mapRow struct {
	name, hint, path string
	size             int64
	items            []diskuse.Spot
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
	tree, at := measureMap(roots, diskFresh)
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
		vr := volumeRows()
		for _, r := range vr {
			volumes += r.size
		}
		rows = append(rows, vr...)
		sort.Slice(rows, func(i, j int) bool { return rows[i].size > rows[j].size })
	}
	if d, ok := sysinfo.DataDisk(); ok && len(args) == 0 {
		ui.Println(ui.Title("занято "+strconv.Itoa(d.Percent())+"%") +
			ui.Dim(", свободно "+fmtBytes(int64(d.Avail))+" из "+fmtBytes(int64(d.Size))))
		ui.Println()
		defer func(rest int64) {
			if rest > 1<<30 {
				ui.Println(ui.Dim("ещё " + fmtBytes(rest) + " не разобрано: то, что без прав не прочитать, снимки Time Machine и копии Chrome, если они есть"))
			}
		}(int64(d.Size-d.Avail) - measured - volumes)
		if n := chromeClones(); n > 0 {
			defer ui.Println(ui.Warn("Chrome держит "+strconv.Itoa(n)+" "+ui.Plural(n, "копию", "копии", "копий")+" самого себя") +
				ui.Dim(" в /private/var/folders/*/X/com.google.Chrome.code_sign_clone - известная ошибка Chrome на macOS."+
					" Сколько они занимают на самом деле, du не скажет: копии APFS делят место, пока Chrome не обновится."+
					" Закрой Chrome и удали эту папку, он создаст её заново"))
		}
	}
	shown := rows
	if !diskAll && len(rows) > 12 {
		shown = rows[:12]
	}
	for _, r := range shown {
		printMapRow(r)
	}
	ui.Println()
	defer ui.Println(ui.Dim("кеши по программам: tim disk --system; зависимости проектов: tim disk --deps, --clean; tim ничего не удаляет сам"))
	if len(shown) < len(rows) {
		ui.Println(ui.Dim("показано " + strconv.Itoa(len(shown)) + " мест из " + strconv.Itoa(len(rows)) + " крупнее 1 GB; все, от 100 MB: tim disk --all"))
	}
	if time.Since(at) > time.Minute {
		ui.Println(ui.Dim("замер " + strconv.Itoa(int(time.Since(at).Minutes())) + " мин назад; заново: tim disk --fresh"))
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
				rows = append(rows, mapRow{name: l.Name, hint: l.Hint, path: group})
			}
			rows[i].size += s.Size
			rows[i].items = append(rows[i].items, s)
			continue
		}
		r := mapRow{path: s.Path, size: s.Size}
		switch {
		case s.Rest:
			r.name = "прочее в " + filepath.Base(s.Path)
		case ok:
			r.name, r.hint = l.Name, l.Hint
		}
		if p, found := cfg.ProjectHere(s.Path); found && r.name == "" {
			r.name = "проект " + p.Name
		}
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].size > rows[j].size })
	return rows
}

func printMapRow(r mapRow) {
	name := r.name
	if name == "" {
		name = filepath.Base(r.path)
	}
	where := ""
	if r.path != "" {
		where = showPath(r.path)
	}
	ui.Println(ui.Label(ui.Pad(fmtBytes(r.size), 10)) + ui.Pad(name, 34) + ui.Dim(where))
	if len(r.items) > 0 {
		var games map[string]diskuse.Game
		if strings.HasSuffix(r.path, "steamapps/common") {
			games = diskuse.SteamGames(r.path)
		}
		var items []diskuse.Spot
		var rest int64
		for _, it := range r.items {
			if it.Rest {
				rest += it.Size
				continue
			}
			items = append(items, it)
		}
		more := 0
		if !diskAll && len(items) > 5 {
			for _, it := range items[5:] {
				rest += it.Size
			}
			more = len(items) - 5
			items = items[:5]
		}
		for _, it := range items {
			label := diskuse.ItemName(it.Path)
			note := ""
			if g, ok := games[filepath.Base(it.Path)]; ok {
				label = g.Name
				note = playedAgo(g.LastPlayed)
			}
			ui.Println("          " + ui.Dim(ui.Pad(fmtBytes(it.Size), 10)) + ui.Pad(label, 32) + note)
		}
		if rest > 0 {
			text := "остальное, по мелочи"
			if more > 0 {
				text = "ещё " + strconv.Itoa(more) + " и мелочь: tim disk --all"
			}
			ui.Println("          " + ui.Dim(ui.Pad(fmtBytes(rest), 10)+text))
		}
	}
	if r.hint != "" {
		ui.Println("          " + ui.Dim(r.hint))
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
	sys := mapRow{name: "macOS: система и служебные тома", hint: "место самой системы, освободить нельзя"}
	var rows []mapRow
	for _, v := range diskuse.ParseAPFS(out, "/System/Volumes/Data") {
		switch v.Role {
		case "Data":
		case "System", "Preboot", "Recovery", "VM", "Update", "xART", "Hardware":
			sys.size += v.Size
		default:
			rows = append(rows, mapRow{name: "том " + v.Name, size: v.Size, hint: "отдельный том на этом же диске; не нужен - Дисковая утилита, удалить том"})
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
