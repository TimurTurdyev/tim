package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Экраны диска: обзор, папки, хвосты. Данные считаются здесь из того же дерева, что и текст.

func uiOptions() tui.Options {
	return tui.Options{Commands: commandEntries, RunCommand: runLine}
}

// runScreen открывает один экран на весь терминал; выбранную папку для cd отдаёт шиму.
func runScreen(s tui.Screen) error {
	dir, err := tui.Run(s, uiOptions())
	if err != nil {
		return err
	}
	if dir != "" {
		if f := os.Getenv("TIM_CD_FILE"); f != "" {
			return os.WriteFile(f, []byte(dir), 0o600)
		}
	}
	return nil
}

// diskState: дерево размеров, общее для обзора и обозревателя; после уборки правится на месте.
type diskState struct {
	mu    sync.Mutex
	roots []string
	tree  diskuse.Tree
	at    time.Time
	args  []string
	fresh bool // --fresh: первый замер мимо кэша
}

func newDiskState(args []string) (*diskState, error) {
	roots := mapRoots()
	if len(args) > 0 {
		r, err := filepath.Abs(filepathExpand(args[0]))
		if err != nil {
			return nil, err
		}
		roots = []string{r}
	}
	return &diskState{roots: roots, args: args, fresh: diskFresh}, nil
}

func (d *diskState) ensure(fresh bool, progress func(string)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tree != nil && !fresh {
		return
	}
	fresh = fresh || d.fresh
	d.fresh = false
	d.tree, d.at = measureMap(d.roots, fresh, func(done, total int, root string) {
		if progress != nil {
			progress(fmt.Sprintf("считаю %s: %d из %d", paths.Show(root), done, total))
		}
	})
}

// remove: в Корзину и поправить дерево, не пересчитывая диск заново.
func (d *diskState) remove(pathsToTrash []string) (freed int64, errs []string) {
	home := paths.Home()
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, p := range pathsToTrash {
		var err error
		if strings.HasPrefix(p, home+"/") {
			err = moveToTrash(p)
		} else {
			err = runner.Sudo("mv", p, filepath.Join(home, ".Trash")+"/")
		}
		if err != nil {
			errs = append(errs, filepath.Base(p)+": "+err.Error())
			continue
		}
		freed += d.tree[p]
		moveInTree(d.tree, p, filepath.Join(home, ".Trash"))
	}
	// Сохранённый замер устарел: следующий обзор из терминала посчитает заново.
	_ = os.Remove(mapCachePath())
	return freed, errs
}

// cliLine: одна перезаписываемая строка хода в stderr - для замера до открытия экрана.
func cliLine(s string) {
	if ui.Terminal() {
		fmt.Fprintf(os.Stderr, "\r\033[2K%s", ui.Dim(s))
	}
}

// ---------- обзор ----------

func diskScreen(args []string) (tui.Screen, error) {
	st, err := newDiskState(args)
	if err != nil {
		return nil, err
	}
	return tui.NewDisk(tui.DiskOpts{
		Load: func(fresh bool, progress func(string)) tui.DiskData {
			st.ensure(fresh, progress)
			return diskData(st)
		},
		Remove:    st.remove,
		Browse:    func() tui.Screen { return browseScreen(st) },
		Leftovers: leftScreen,
	}), nil
}

// diskData: вкладки обзора из дерева - те же места, что печатает tim disk.
func diskData(st *diskState) tui.DiskData {
	st.mu.Lock()
	defer st.mu.Unlock()
	home := paths.Home()
	stop := func(p string) bool {
		l, _, ok := diskuse.LabelOf(p, home)
		return ok && l.Stop
	}
	var spots []diskuse.Spot
	for _, r := range st.roots {
		spots = append(spots, diskuse.Spots(st.tree, r, 100<<20, stop)...)
	}
	rows := groupSpots(spots, home)
	if len(st.args) == 0 {
		rows = append(rows, volumeRows()...)
		sort.Slice(rows, func(i, j int) bool { return rows[i].size > rows[j].size })
	}
	data := tui.DiskData{MeasuredAt: st.at}
	if d, ok := sysinfo.DataDisk(); ok {
		data.UsedPct, data.Avail, data.Size = d.Percent(), int64(d.Avail), int64(d.Size)
	}
	titles := map[diskuse.Kind]string{diskuse.Free: "Можно освободить", diskuse.Apps: "Программы", diskuse.Data: "Твои файлы", diskuse.System: "Система"}
	for _, kind := range []diskuse.Kind{diskuse.Free, diskuse.Apps, diskuse.Data, diskuse.System} {
		tab := tui.DiskTab{Title: titles[kind]}
		for _, r := range rows {
			if r.kind != kind {
				continue
			}
			tab.Total += r.size
			tab.Rows = append(tab.Rows, diskRow(r, home))
		}
		if len(tab.Rows) > 0 {
			data.Tabs = append(data.Tabs, tab)
		}
	}
	if n := chromeClones(); n > 0 && len(st.args) == 0 {
		data.Notes = append(data.Notes, "Chrome хранит "+strconv.Itoa(n)+" "+ui.Plural(n, "копию", "копии", "копий")+
			" самого себя (известная ошибка): закрой Chrome и убери /private/var/folders/*/*/X/com.google.Chrome.code_sign_clone")
	}
	return data
}

func diskRow(r mapRow, home string) tui.DiskRow {
	name := r.name
	if name == "" {
		name = filepath.Base(r.path)
	}
	row := tui.DiskRow{Name: name, Path: r.path, Hint: r.hint, Size: r.size}
	if r.path != "" {
		row.Show = paths.Show(r.path)
		// Целиком в Корзину - кэши, которые вернутся сами, и свои файлы в домашней папке.
		// Данные программ (OrbStack, Steam) и систему так не трогаем.
		l, _, known := diskuse.LabelOf(r.path, home)
		row.Removable = strings.HasPrefix(r.path, home+"/") && ((known && l.Whole) || (r.kind == diskuse.Data && !l.Stop))
	}
	var games map[string]diskuse.Game
	if strings.HasSuffix(r.path, "steamapps/common") {
		games = diskuse.SteamGames(r.path)
		if !steamInstalled() {
			row.Warn = "Steam удалён, а игры остались: можно убрать папку Steam целиком"
			row.Hint = "trash " + filepath.Dir(filepath.Dir(r.path))
		}
	}
	var rest int64
	for _, it := range r.items {
		if it.Rest {
			rest += it.Size
			continue
		}
		item := tui.DiskItem{Name: diskuse.ItemName(it.Path), Size: it.Size}
		if g, ok := games[filepath.Base(it.Path)]; ok {
			item.Name, item.Note = g.Name, playedAgo(g.LastPlayed)
		}
		row.Items = append(row.Items, item)
	}
	sort.Slice(row.Items, func(i, j int) bool { return row.Items[i].Size > row.Items[j].Size })
	if rest > 0 && len(row.Items) > 0 {
		row.Items = append(row.Items, tui.DiskItem{Name: "остальное, по мелочи", Size: rest})
	}
	return row
}

// ---------- папки ----------

func browseScreen(st *diskState) tui.Screen {
	home := paths.Home()
	entry := func(p string, size int64) tui.Entry {
		e := tui.Entry{Name: filepath.Base(p), Path: p, Size: size, Dir: true}
		if l, _, ok := diskuse.LabelOf(p, home); ok {
			e.Hint = l.Name
			if l.Hint != "" {
				e.Hint += " \u2192 " + l.Hint
			}
		}
		return e
	}
	var vols []mapRow
	if len(st.args) == 0 {
		vols = volumeRows()
	}
	var used int64
	if d, ok := sysinfo.DataDisk(); ok && len(st.args) == 0 {
		used = int64(d.Size - d.Avail)
	}
	top := func() []tui.Entry {
		st.mu.Lock()
		defer st.mu.Unlock()
		var list []tui.Entry
		var sum int64
		for _, r := range st.roots {
			e := entry(r, st.tree[r])
			e.Name = paths.Show(r)
			list = append(list, e)
			sum += st.tree[r]
		}
		for _, v := range vols {
			list = append(list, tui.Entry{Name: v.name, Size: v.size, Hint: v.hint})
			sum += v.size
		}
		if rest := used - sum; rest > 1<<30 && used > 0 {
			list = append(list, tui.Entry{Name: "не разобрано", Size: rest, Hint: "без прав не прочитать, снимки Time Machine, копии Chrome"})
		}
		return sortEntries(list)
	}
	root := tui.Entry{Name: "диск", Dir: true, Size: used}
	if len(st.args) > 0 {
		st.mu.Lock()
		root = entry(st.roots[0], st.tree[st.roots[0]])
		st.mu.Unlock()
		root.Name = paths.Show(st.roots[0])
	}
	kids := func(p string) []tui.Entry {
		// Зовётся в фоне, поэтому холодный замер здесь не вешает экран.
		st.ensure(false, nil)
		if p == "" {
			return top()
		}
		st.mu.Lock()
		size := st.tree[p]
		var list []tui.Entry
		var sum int64
		for k, v := range st.tree {
			if filepath.Dir(k) == p && k != p {
				list = append(list, entry(k, v))
				sum += v
			}
		}
		st.mu.Unlock()
		// В кэше только папки от 50 MB и до глубины замера: внутри большой папки без детей меряем сами.
		if len(list) == 0 && size > 100<<20 {
			out, _ := runner.Output("du", "-xk", "-d", "1", p)
			fresh := diskuse.ParseDuAbs(out)
			st.mu.Lock()
			list, sum = nil, 0
			for k, v := range fresh {
				if filepath.Dir(k) == p && k != p {
					st.tree[k] = v
					list = append(list, entry(k, v))
					sum += v
				}
			}
			st.mu.Unlock()
		}
		if rest := size - sum; rest > 1<<20 {
			list = append(list, tui.Entry{Name: "файлы в этой папке и мелкие папки", Size: rest})
		}
		return sortEntries(list)
	}
	open := func(p string) {
		if runtime.GOOS == "darwin" {
			_, _ = runner.Output("open", "-R", p)
			return
		}
		_, _ = runner.Output("xdg-open", filepath.Dir(p))
	}
	remove := func(list []tui.Entry) (int64, []string) {
		var ps []string
		for _, e := range list {
			ps = append(ps, e.Path)
		}
		return st.remove(ps)
	}
	return tui.NewBrowse(tui.BrowseOpts{Root: root, Kids: kids, Open: open, Remove: remove, Total: used})
}

// ---------- хвосты ----------

func leftScreen() tui.Screen {
	return tui.NewLeftovers(tui.LeftOpts{
		Load: func() []tui.LeftGroup {
			groups, err := gatherLeftovers()
			if err != nil {
				return nil
			}
			var out []tui.LeftGroup
			for _, g := range groups {
				lg := tui.LeftGroup{ID: g.owner, Name: groupName(g), Owner: g.owner, Size: g.size, Sure: g.sure}
				switch {
				case g.similar != "":
					lg.Note = "установлен другой " + g.similar + ": данные другой версии"
				case !g.sure:
					lg.Note = "похоже, проверь"
				case len(g.agents) > 0:
					lg.Note = "автозапуск без программы"
				case len(g.pids) > 0:
					lg.Note = "процесс ещё работает, хотя программа удалена"
				default:
					lg.Note = "программа удалена"
				}
				lg.Hidden = g.size < 50<<20 && len(g.agents) == 0 && len(g.pids) == 0
				for _, it := range g.items {
					li := tui.LeftItem{Path: libPath(it.path), Size: it.size, Note: it.note}
					if it.path == "" {
						li.Path = it.note
						li.Note = ""
					}
					lg.Items = append(lg.Items, li)
				}
				sort.Slice(lg.Items, func(i, j int) bool { return lg.Items[i].Size > lg.Items[j].Size })
				lg.Commands = leftCommands(g)
				out = append(out, lg)
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
			return out
		},
		Remove: func(lg tui.LeftGroup) error {
			groups, err := gatherLeftovers()
			if err != nil {
				return err
			}
			for _, g := range groups {
				if g.owner == lg.ID {
					if !removeGroup(g) {
						return fmt.Errorf("убрано не всё")
					}
					return nil
				}
			}
			return nil
		},
	})
}

// leftCommands: что выполнит уборка группы - для окна подтверждения.
func leftCommands(g *leftGroup) []string {
	var out []string
	for _, a := range g.agents {
		if strings.HasPrefix(a, "system/") {
			out = append(out, "sudo launchctl bootout "+a)
		} else {
			out = append(out, "launchctl bootout "+a)
		}
	}
	if len(g.pids) > 0 {
		out = append(out, "kill "+strings.Join(g.pids, " "))
	}
	mine, system := splitOwn(g)
	if len(mine) > 0 {
		out = append(out, "trash "+strings.Join(mine, " "))
	}
	for _, p := range system {
		out = append(out, "sudo mv "+p+" ~/.Trash/")
	}
	return out
}
