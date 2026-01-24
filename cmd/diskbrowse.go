package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var diskBrowse bool

// diskBrowseRun: обозреватель по папкам. Корень - весь занятый диск: домашняя папка, программы,
// тома и то, что не разобрать, так что сумма сходится с df и видно, куда копать.
func diskBrowseRun(args []string) error {
	roots := mapRoots()
	if len(args) > 0 {
		r, err := filepath.Abs(filepathExpand(args[0]))
		if err != nil {
			return err
		}
		roots = []string{r}
	}
	tree, _ := measureMap(roots, diskFresh)
	var mu sync.Mutex
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
	var used int64
	rootEntry := tui.Entry{Name: "диск", Dir: true}
	var top []tui.Entry
	for _, r := range roots {
		e := entry(r, tree[r])
		e.Name = showPath(r)
		top = append(top, e)
		rootEntry.Size += tree[r]
	}
	if len(args) == 0 {
		for _, v := range volumeRows() {
			top = append(top, tui.Entry{Name: v.name, Size: v.size, Hint: v.hint})
			rootEntry.Size += v.size
		}
		if d, ok := sysinfo.DataDisk(); ok {
			used = int64(d.Size - d.Avail)
			if rest := used - rootEntry.Size; rest > 1<<30 {
				top = append(top, tui.Entry{Name: "не разобрано", Size: rest, Hint: "без прав не прочитать, снимки Time Machine, копии Chrome"})
			}
			rootEntry.Size = used
		}
	} else if len(roots) == 1 {
		rootEntry = entry(roots[0], tree[roots[0]])
		rootEntry.Name = showPath(roots[0])
	}
	kids := func(p string) []tui.Entry {
		if p == "" {
			return sortEntries(top)
		}
		mu.Lock()
		size := tree[p]
		var list []tui.Entry
		var sum int64
		for k, v := range tree {
			if filepath.Dir(k) == p && k != p {
				list = append(list, entry(k, v))
				sum += v
			}
		}
		mu.Unlock()
		// В кэше только папки от 50 MB и до глубины замера: если внутри большой папки ничего
		// не видно, меряем её саму на уровень вглубь.
		if len(list) == 0 && size > 100<<20 {
			out, _ := runner.Output("du", "-xk", "-d", "1", p)
			fresh := diskuse.ParseDuAbs(out)
			mu.Lock()
			list, sum = nil, 0
			for k, v := range fresh {
				if filepath.Dir(k) == p && k != p {
					tree[k] = v
					list = append(list, entry(k, v))
					sum += v
				}
			}
			mu.Unlock()
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
	marked, trash, err := tui.Browse(tui.BrowseOpts{Root: rootEntry, Kids: kids, Open: open, Total: used})
	if err != nil || !trash || len(marked) == 0 {
		return err
	}
	sort.Slice(marked, func(i, j int) bool { return marked[i].Size > marked[j].Size })
	var sum int64
	for _, e := range marked {
		ui.Println(ui.Label(ui.Pad(fmtBytes(e.Size), 10)) + showPath(e.Path))
		sum += e.Size
	}
	if !runner.ConfirmDefault("убрать это в Корзину ("+fmtBytes(sum)+")?", false) {
		return nil
	}
	var freed int64
	for _, e := range marked {
		var err error
		if strings.HasPrefix(e.Path, home+"/") {
			err = moveToTrash(e.Path)
		} else {
			err = runner.Sudo("mv", e.Path, filepath.Join(home, ".Trash")+"/")
		}
		if err != nil {
			ui.Println(ui.Bad(e.Path + ": " + err.Error()))
			continue
		}
		freed += e.Size
	}
	// Замер устарел: следующий обзор посчитает заново.
	_ = os.Remove(mapCachePath())
	ui.Println(ui.Info("в Корзине "+fmtBytes(freed)) + ui.Dim("  место освободится, когда очистишь Корзину"))
	return nil
}

func sortEntries(list []tui.Entry) []tui.Entry {
	sort.Slice(list, func(i, j int) bool { return list[i].Size > list[j].Size })
	return list
}
