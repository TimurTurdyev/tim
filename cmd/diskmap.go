package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	diskAll   bool
	diskFresh bool
)

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

func diskMap(args []string) error {
	roots := diskuse.DefaultRoots()
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
	tree, at := diskuse.Map(roots, diskFresh, cliProgress())
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
	rows := diskuse.Rows(spots, home, projectOwner)
	var volumes int64
	if len(args) == 0 {
		for _, r := range diskuse.VolumeRows() {
			volumes += r.Size
			rows = append(rows, r)
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Size > rows[j].Size })
	}
	var unknown int64
	if d, ok := sysinfo.DataDisk(); ok && len(args) == 0 {
		pct := d.Percent()
		ui.Println(ui.Title("Диск") + "  " + ui.Bar(pct, 30) + ui.Dim("свободно ") + ui.Bytes(int64(d.Avail)) + ui.Dim(" из "+ui.Bytes(int64(d.Size))))
		unknown = int64(d.Size-d.Avail) - measured - volumes
	}
	// Кратко - по несколько самых больших в разделе; освобождаемого больше: с него и начинают.
	limit := map[diskuse.Kind]int{diskuse.Free: 8, diskuse.Apps: 3, diskuse.Data: 6, diskuse.System: 2}
	var shown []diskuse.Row
	taken := map[diskuse.Kind]int{}
	for _, r := range rows {
		if diskAll || taken[r.Kind] < limit[r.Kind] {
			shown = append(shown, r)
			taken[r.Kind]++
		}
	}
	var top int64
	if len(rows) > 0 {
		top = rows[0].Size
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
		var in []diskuse.Row
		var total int64
		for _, r := range rows {
			if r.Kind == sec.kind {
				total += r.Size
			}
		}
		for _, r := range shown {
			if r.Kind == sec.kind {
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
	if n := diskuse.ChromeClones(); n > 0 && len(args) == 0 {
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

// sizeBar: короткая полоска размера места относительно самого большого - глазом видно, что важно.
func sizeBar(size, top int64) string {
	const w = 8
	n := 1
	if top > 0 {
		n = max(1, int(size*w/top))
	}
	return ui.Info(strings.Repeat("\u2588", n)) + ui.Dim(strings.Repeat("\u2591", w-n))
}

func printMapRow(r diskuse.Row, top int64) {
	name := r.Name
	if name == "" {
		name = filepath.Base(r.Path)
	}
	where := ""
	if r.Path != "" {
		where = paths.Show(r.Path)
	}
	ui.Println("  " + ui.Label(ui.Pad(ui.Bytes(r.Size), 9)) + sizeBar(r.Size, top) + "  " + ui.Pad(ui.Clip(name, 38), 40) + ui.Dim(where))
	const indent = "                       "
	if len(r.Items) > 0 {
		var games map[string]diskuse.Game
		if strings.HasSuffix(r.Path, "steamapps/common") {
			games = diskuse.SteamGames(r.Path)
		}
		var parts []string
		var rest int64
		more := 0
		for _, it := range r.Items {
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
				played, stale := diskuse.PlayedAgo(g.LastPlayed)
				if stale {
					played = ui.Warn(played)
				} else {
					played = ui.Dim(played)
				}
				label = g.Name + " " + ui.Bytes(it.Size) + ", " + played
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
		if len(inline) > 1 || (len(inline) == 1 && len(r.Items) > 1) || (len(inline) == 1 && !strings.HasSuffix(inline[0], ui.Bytes(r.Size))) {
			ui.Println(indent + ui.Dim(strings.Join(inline, "  \u00b7  ")))
		} else if len(inline) == 1 {
			ui.Println(indent + ui.Dim(strings.TrimSuffix(inline[0], " "+ui.Bytes(r.Size))))
		}
	}
	if strings.HasSuffix(r.Path, "steamapps/common") && !diskuse.SteamInstalled() {
		// Удаление Steam игры не трогает: они так и лежат, а удалить их через Steam уже нечем.
		lib := filepath.Dir(filepath.Dir(r.Path))
		ui.Println(indent + ui.Warn("Steam удалён, а игры остались") + ui.Dim(": trash "+shellq.Join([]string{lib})))
		return
	}
	if r.Hint != "" {
		ui.Println(indent + ui.Dim("\u2192 "+r.Hint))
	}
}

// projectOwner: чей проект лежит по пути - для подписей мест.
func projectOwner(path string) string {
	if p, ok := cfg.ProjectHere(path); ok {
		return p.Name
	}
	return ""
}
