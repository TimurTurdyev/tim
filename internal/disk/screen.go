package disk

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/leftover"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/trash"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// tabTitles: вкладки обзора - те же разделы, что печатает tim disk.
var tabTitles = map[diskuse.Kind]string{diskuse.Free: "Можно освободить", diskuse.Apps: "Программы", diskuse.Data: "Твои файлы", diskuse.System: "Система"}

// state: дерево размеров, общее для обзора и обозревателя; после уборки правится на месте.
type state struct {
	mu    sync.Mutex
	cfg   *config.Config
	roots []string
	tree  diskuse.Tree
	at    time.Time
	args  []string
	fresh bool // --fresh: первый замер мимо кэша
}

func newState(cfg *config.Config, args []string, fresh bool) (*state, error) {
	roots, err := rootsOf(args)
	if err != nil {
		return nil, err
	}
	return &state{cfg: cfg, roots: roots, args: args, fresh: fresh}, nil
}

func (d *state) ensure(fresh bool, progress func(string)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tree != nil && !fresh {
		return
	}
	fresh = fresh || d.fresh
	d.fresh = false
	d.tree, d.at = diskuse.Map(d.roots, fresh, func(done, total int, root string) {
		if progress != nil {
			progress(fmt.Sprintf("считаю %s: %d из %d", paths.Show(root), done, total))
		}
	})
}

// remove: в Корзину и поправить дерево, не пересчитывая диск заново.
func (d *state) remove(pathsToTrash []string) (freed int64, errs []string) {
	home := paths.Home()
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, p := range pathsToTrash {
		var err error
		if strings.HasPrefix(p, home+"/") {
			err = trash.Move(p)
		} else {
			err = runner.Sudo("mv", p, filepath.Join(home, ".Trash")+"/")
		}
		if err != nil {
			errs = append(errs, filepath.Base(p)+": "+err.Error())
			continue
		}
		freed += d.tree[p]
		d.tree.MoveInTree(p, filepath.Join(home, ".Trash"))
	}
	// Сохранённый замер устарел: следующий обзор из терминала посчитает заново.
	if !runner.DryRun {
		_ = os.Remove(diskuse.CachePath())
	}
	return freed, errs
}

// Screen: обзор диска по вкладкам; args - папка вместо всего диска.
func Screen(env *app.Env, args []string, fresh bool) (tui.Screen, error) {
	st, err := newState(env.Cfg, args, fresh)
	if err != nil {
		return nil, err
	}
	return tui.NewDisk(tui.DiskOpts{
		Load: func(fresh bool, progress func(string)) tui.DiskData {
			st.ensure(fresh, progress)
			return overviewData(st)
		},
		Remove:    st.remove,
		Browse:    func() tui.Screen { return browseScreen(st) },
		Leftovers: func() tui.Screen { return LeftoversScreen(env.Cfg) },
	}), nil
}

// BrowseScreen: обозреватель по папкам от всего занятого диска.
func BrowseScreen(env *app.Env) (tui.Screen, error) {
	st, err := newState(env.Cfg, nil, false)
	if err != nil {
		return nil, err
	}
	return browseScreen(st), nil
}

// browse: обозреватель из командной строки. Замер до экрана: ход печатается в stderr, пока
// терминал ещё не занят.
func browse(env *app.Env, args []string, fresh bool) error {
	st, err := newState(env.Cfg, args, fresh)
	if err != nil {
		return err
	}
	st.ensure(false, func(s string) {
		if ui.Terminal() {
			fmt.Fprintf(os.Stderr, "\r\033[2K%s", ui.Dim(s))
		}
	})
	fmt.Fprint(os.Stderr, "\r\033[2K")
	return env.Open(browseScreen(st))
}

// overviewData: вкладки обзора из дерева - те же места, что печатает tim disk.
func overviewData(st *state) tui.DiskData {
	st.mu.Lock()
	defer st.mu.Unlock()
	home := paths.Home()
	var spots []diskuse.Spot
	for _, r := range st.roots {
		spots = append(spots, diskuse.Spots(st.tree, r, 100<<20, stopAt(home))...)
	}
	rows := diskuse.Rows(spots, home, ownerOf(st.cfg))
	if len(st.args) == 0 {
		rows = append(rows, diskuse.VolumeRows()...)
		sortRows(rows)
	}
	data := tui.DiskData{MeasuredAt: st.at}
	if d, ok := sysinfo.DataDisk(); ok {
		data.UsedPct, data.Avail, data.Size = d.Percent(), int64(d.Avail), int64(d.Size)
	}
	for _, sec := range sections {
		tab := tui.DiskTab{Title: tabTitles[sec.kind]}
		for _, r := range rows {
			if r.Kind == sec.kind {
				tab.Total += r.Size
				tab.Rows = append(tab.Rows, diskRow(r, home))
			}
		}
		if len(tab.Rows) > 0 {
			data.Tabs = append(data.Tabs, tab)
		}
	}
	if n := diskuse.ChromeClones(); n > 0 && len(st.args) == 0 {
		data.Notes = append(data.Notes, "Chrome хранит "+ui.Count(n, "копию", "копии", "копий")+
			" самого себя (известная ошибка): закрой Chrome и убери /private/var/folders/*/*/X/com.google.Chrome.code_sign_clone")
	}
	return data
}

func diskRow(r diskuse.Row, home string) tui.DiskRow {
	name := r.Name
	if name == "" {
		name = filepath.Base(r.Path)
	}
	row := tui.DiskRow{Name: name, Path: r.Path, Hint: r.Hint, Size: r.Size}
	if r.Path != "" {
		row.Show = paths.Show(r.Path)
		// Целиком в Корзину - кэши, которые вернутся сами, и свои файлы в домашней папке.
		// Данные программ (OrbStack, Steam) и систему так не трогаем.
		l, _, known := diskuse.LabelOf(r.Path, home)
		row.Removable = strings.HasPrefix(r.Path, home+"/") && ((known && l.Whole) || (r.Kind == diskuse.Data && !l.Stop))
	}
	var games map[string]diskuse.Game
	if strings.HasSuffix(r.Path, "steamapps/common") {
		games = diskuse.SteamGames(r.Path)
		if !diskuse.SteamInstalled() {
			row.Warn = "Steam удалён, а игры остались: можно убрать папку Steam целиком"
			row.Hint = "trash " + filepath.Dir(filepath.Dir(r.Path))
		}
	}
	var rest int64
	for _, it := range r.Items {
		if it.Rest {
			rest += it.Size
			continue
		}
		item := tui.DiskItem{Name: diskuse.ItemName(it.Path), Size: it.Size}
		if g, ok := games[filepath.Base(it.Path)]; ok {
			item.Name = g.Name
			item.Note, item.Stale = diskuse.PlayedAgo(g.LastPlayed)
		}
		row.Items = append(row.Items, item)
	}
	sort.SliceStable(row.Items, func(i, j int) bool { return row.Items[i].Size > row.Items[j].Size })
	if rest > 0 && len(row.Items) > 0 {
		row.Items = append(row.Items, tui.DiskItem{Name: "остальное, по мелочи", Size: rest})
	}
	return row
}

func browseScreen(st *state) tui.Screen {
	home := paths.Home()
	var used int64
	if d, ok := sysinfo.DataDisk(); ok && len(st.args) == 0 {
		used = int64(d.Size - d.Avail)
	}
	root := tui.Entry{Name: "диск", Dir: true, Size: used}
	if len(st.args) > 0 {
		st.mu.Lock()
		root = entry(st.roots[0], st.tree[st.roots[0]], home)
		st.mu.Unlock()
		root.Name = paths.Show(st.roots[0])
	}
	remove := func(list []tui.Entry) (int64, []string) {
		var ps []string
		for _, e := range list {
			ps = append(ps, e.Path)
		}
		return st.remove(ps)
	}
	return tui.NewBrowse(tui.BrowseOpts{Root: root, Kids: st.kidsFunc(home, used), Open: reveal, Remove: remove, Total: used})
}

// kidsFunc: содержимое папки для обозревателя; пустой путь - верх: корни, тома и неразобранное.
func (d *state) kidsFunc(home string, used int64) func(string) []tui.Entry {
	var vols []diskuse.Row
	if len(d.args) == 0 {
		vols = diskuse.VolumeRows()
	}
	return func(p string) []tui.Entry {
		// Зовётся в фоне, поэтому холодный замер здесь не вешает экран.
		d.ensure(false, nil)
		if p == "" {
			return d.top(home, vols, used)
		}
		return d.children(p, home)
	}
}

func (d *state) top(home string, vols []diskuse.Row, used int64) []tui.Entry {
	d.mu.Lock()
	defer d.mu.Unlock()
	var list []tui.Entry
	var sum int64
	for _, r := range d.roots {
		e := entry(r, d.tree[r], home)
		e.Name = paths.Show(r)
		list = append(list, e)
		sum += d.tree[r]
	}
	for _, v := range vols {
		list = append(list, tui.Entry{Name: v.Name, Size: v.Size, Hint: v.Hint})
		sum += v.Size
	}
	if rest := used - sum; rest > 1<<30 && used > 0 {
		list = append(list, tui.Entry{Name: "не разобрано", Size: rest, Hint: "без прав не прочитать, снимки Time Machine, копии Chrome"})
	}
	return bySize(list)
}

func (d *state) children(p, home string) []tui.Entry {
	d.mu.Lock()
	size := d.tree[p]
	var list []tui.Entry
	var sum int64
	for k, v := range d.tree {
		if filepath.Dir(k) == p && k != p {
			list = append(list, entry(k, v, home))
			sum += v
		}
	}
	d.mu.Unlock()
	// В кэше только папки от 50 MB и до глубины замера: внутри большой папки без детей меряем сами.
	if len(list) == 0 && size > 100<<20 {
		out, _ := runner.OutputWithin(0, "du", "-xk", "-d", "1", p)
		fresh := diskuse.ParseDuAbs(out)
		d.mu.Lock()
		list, sum = nil, 0
		for k, v := range fresh {
			if filepath.Dir(k) == p && k != p {
				d.tree[k] = v
				list = append(list, entry(k, v, home))
				sum += v
			}
		}
		d.mu.Unlock()
	}
	if rest := size - sum; rest > 1<<20 {
		list = append(list, tui.Entry{Name: "файлы в этой папке и мелкие папки", Size: rest})
	}
	return bySize(list)
}

func entry(p string, size int64, home string) tui.Entry {
	e := tui.Entry{Name: filepath.Base(p), Path: p, Size: size, Dir: true}
	if l, _, ok := diskuse.LabelOf(p, home); ok {
		e.Hint = l.Name
		if l.Hint != "" {
			e.Hint += " \u2192 " + l.Hint
		}
	}
	return e
}

// reveal: показать папку в Finder или файловом менеджере.
func reveal(p string) {
	if runtime.GOOS == "darwin" {
		_, _ = runner.Output("open", "-R", p)
		return
	}
	_, _ = runner.Output("xdg-open", filepath.Dir(p))
}

func bySize(list []tui.Entry) []tui.Entry {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Size != list[j].Size {
			return list[i].Size > list[j].Size
		}
		return list[i].Name < list[j].Name
	})
	return list
}

// LeftoversScreen: хвосты удалённых программ - отметить и убрать.
func LeftoversScreen(cfg *config.Config) tui.Screen {
	return tui.NewLeftovers(tui.LeftOpts{
		Load: func() []tui.LeftGroup {
			groups, err := gatherLeftovers(cfg)
			if err != nil {
				return nil
			}
			out := make([]tui.LeftGroup, 0, len(groups))
			for _, g := range groups {
				out = append(out, leftGroup(g))
			}
			sort.SliceStable(out, func(i, j int) bool { return out[i].Size > out[j].Size })
			return out
		},
		Remove: func(lg tui.LeftGroup) error {
			groups, err := gatherLeftovers(cfg)
			if err != nil {
				return err
			}
			for _, g := range groups {
				if g.Owner == lg.ID && !leftover.Remove(g) {
					return errors.New("убрано не всё")
				}
			}
			return nil
		},
	})
}

func leftGroup(g *leftover.Group) tui.LeftGroup {
	lg := tui.LeftGroup{ID: g.Owner, Name: g.Name(), Owner: g.Owner, Size: g.Size, Sure: g.Sure, Hidden: minor(g)}
	switch {
	case g.Similar != "":
		lg.Note = "установлен другой " + g.Similar + ": данные другой версии"
	case !g.Sure:
		lg.Note = "похоже, проверь"
	case len(g.Agents) > 0:
		lg.Note = "автозапуск без программы"
	case len(g.PIDs) > 0:
		lg.Note = "процесс ещё работает, хотя программа удалена"
	default:
		lg.Note = "программа удалена"
	}
	for _, it := range g.Items {
		li := tui.LeftItem{Path: leftover.LibPath(it.Path), Size: it.Size, Note: it.Note}
		if it.Path == "" {
			li.Path, li.Note = it.Note, ""
		}
		lg.Items = append(lg.Items, li)
	}
	sort.SliceStable(lg.Items, func(i, j int) bool { return lg.Items[i].Size > lg.Items[j].Size })
	lg.Commands = leftCommands(g)
	return lg
}

// leftCommands: что выполнит уборка группы - для окна подтверждения.
func leftCommands(g *leftover.Group) []string {
	var out []string
	for _, a := range g.Agents {
		if strings.HasPrefix(a, "system/") {
			out = append(out, "sudo launchctl bootout "+a)
		} else {
			out = append(out, "launchctl bootout "+a)
		}
	}
	if len(g.PIDs) > 0 {
		out = append(out, "kill "+strings.Join(g.PIDs, " "))
	}
	mine, system := g.SplitOwn()
	if len(mine) > 0 {
		out = append(out, "trash "+strings.Join(mine, " "))
	}
	for _, p := range system {
		out = append(out, "sudo mv "+p+" ~/.Trash/")
	}
	return out
}
