package disk

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// sections: разделы обзора по порядку; освобождаемое первым - с него и начинают.
var sections = []struct {
	kind  diskuse.Kind
	title string
}{
	{diskuse.Free, "Можно освободить"},
	{diskuse.Apps, "Программы"},
	{diskuse.Data, "Твои файлы - решать тебе"},
	{diskuse.System, "Система"},
}

// shortLimit: сколько мест раздела показывает краткий обзор.
var shortLimit = map[diskuse.Kind]int{diskuse.Free: 8, diskuse.Apps: 3, diskuse.Data: 6, diskuse.System: 2}

// indent: отступ строк разбивки под строкой места.
const indent = "                       "

// printOverview: занято и свободно и самые большие места по разделам; all - все от 100 MB.
func printOverview(cfg *config.Config, args []string, all, fresh, asJSON bool) error {
	roots, err := rootsOf(args)
	if err != nil {
		return err
	}
	minSize := int64(1 << 30)
	if all {
		minSize = 100 << 20
	}
	tree, at := diskuse.Map(roots, fresh, cliProgress())
	home := paths.Home()
	var spots []diskuse.Spot
	var measured int64
	for _, r := range roots {
		spots = append(spots, diskuse.Spots(tree, r, minSize, stopAt(home))...)
		measured += tree[r]
	}
	rows := diskuse.Rows(spots, home, ownerOf(cfg))
	var volumes int64
	if len(args) == 0 {
		for _, r := range diskuse.VolumeRows() {
			volumes += r.Size
			rows = append(rows, r)
		}
		sortRows(rows)
	}
	var unknown int64
	d, haveDisk := sysinfo.DataDisk()
	if asJSON {
		out := struct {
			Size  int64         `json:"size,omitempty"`
			Avail int64         `json:"avail,omitempty"`
			Rows  []diskuse.Row `json:"rows"`
		}{Rows: rows}
		if haveDisk {
			out.Size, out.Avail = int64(d.Size), int64(d.Avail)
		}
		return ui.JSON(out)
	}
	if haveDisk && len(args) == 0 {
		ui.Println(ui.Title("Диск") + "  " + ui.Bar(d.Percent(), 30) + ui.Dim("свободно ") + ui.Bytes(int64(d.Avail)) + ui.Dim(" из "+ui.Bytes(int64(d.Size))))
		unknown = int64(d.Size-d.Avail) - measured - volumes
	}
	shown := shortList(rows, all)
	printSections(rows, shown, all)
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
		ui.Println(ui.Warn("Chrome хранит "+ui.Count(n, "копию", "копии", "копий")+" самого себя") +
			ui.Dim(" - известная ошибка. Закрой Chrome и: trash /private/var/folders/*/*/X/com.google.Chrome.code_sign_clone"))
	}
	if unknown > 1<<30 {
		ui.Println(ui.Dim("не разобрано " + ui.Bytes(unknown) + ": без прав не прочитать, снимки Time Machine, копии Chrome"))
	}
	return nil
}

// shortList: в кратком обзоре - по несколько самых больших мест в разделе.
func shortList(rows []diskuse.Row, all bool) []diskuse.Row {
	var shown []diskuse.Row
	taken := map[diskuse.Kind]int{}
	for _, r := range rows {
		if all || taken[r.Kind] < shortLimit[r.Kind] {
			shown = append(shown, r)
			taken[r.Kind]++
		}
	}
	return shown
}

func printSections(rows, shown []diskuse.Row, all bool) {
	var top int64
	if len(rows) > 0 {
		top = rows[0].Size
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
			printRow(r, top, all)
		}
	}
}

func printRow(r diskuse.Row, top int64, all bool) {
	name := r.Name
	if name == "" {
		name = filepath.Base(r.Path)
	}
	where := ""
	if r.Path != "" {
		where = paths.Show(r.Path)
	}
	ui.Println("  " + ui.Label(ui.Pad(ui.Bytes(r.Size), 9)) + sizeBar(r.Size, top) + "  " + ui.Pad(ui.Clip(name, 38), 40) + ui.Dim(where))
	if len(r.Items) > 0 {
		printItems(r, all)
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

// printItems: разбивка места - игры отдельными строками, остальное одной строкой.
func printItems(r diskuse.Row, all bool) {
	var games map[string]diskuse.Game
	if strings.HasSuffix(r.Path, "steamapps/common") {
		games = diskuse.SteamGames(r.Path)
	}
	var inline []string
	var rest int64
	listed, more := 0, 0
	for _, it := range r.Items {
		switch {
		case it.Rest:
			rest += it.Size
			continue
		case !all && listed >= 4:
			rest += it.Size
			more++
			continue
		}
		listed++
		if g, ok := games[filepath.Base(it.Path)]; ok {
			ui.Println(indent + g.Name + " " + ui.Bytes(it.Size) + ", " + playedText(g.LastPlayed))
			continue
		}
		inline = append(inline, diskuse.ItemName(it.Path)+" "+ui.Bytes(it.Size))
	}
	if more > 0 {
		inline = append(inline, "ещё "+strconv.Itoa(more))
	} else if rest > 0 && len(inline) > 0 {
		inline = append(inline, "мелочь "+ui.Bytes(rest))
	}
	whole := " " + ui.Bytes(r.Size)
	switch {
	// Одна виртуалка или программа в группе - имя и так понятно из строки, разбивка лишняя.
	case len(inline) == 1 && len(r.Items) == 1 && strings.HasSuffix(inline[0], whole):
		ui.Println(indent + ui.Dim(strings.TrimSuffix(inline[0], whole)))
	case len(inline) > 0:
		ui.Println(indent + ui.Dim(strings.Join(inline, "  \u00b7  ")))
	}
}

func playedText(t time.Time) string {
	played, stale := diskuse.PlayedAgo(t)
	if stale {
		return ui.Warn(played)
	}
	return ui.Dim(played)
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

// rootsOf: что мерить - папку из аргумента или всё, что занято.
func rootsOf(args []string) ([]string, error) {
	if len(args) == 0 {
		return diskuse.DefaultRoots(), nil
	}
	r, err := filepath.Abs(config.ExpandHome(args[0]))
	if err != nil {
		return nil, err
	}
	return []string{r}, nil
}

// stopAt: внутрь известных мест (Library, виртуалка) обзор не спускается.
func stopAt(home string) func(string) bool {
	return func(p string) bool {
		l, _, ok := diskuse.LabelOf(p, home)
		return ok && l.Stop
	}
}

// ownerOf: чей проект лежит по пути - для подписей мест.
func ownerOf(cfg *config.Config) func(string) string {
	return func(path string) string {
		if p, ok := cfg.ProjectHere(path); ok {
			return p.Name
		}
		return ""
	}
}

func sortRows(rows []diskuse.Row) {
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Size > rows[j].Size })
}
