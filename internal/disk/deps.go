package disk

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/trash"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// deps: node_modules, vendor, venv и сборки по проектам; с clean - заброшенные в Корзину.
func deps(env *app.Env, o options, args []string) error {
	root := env.Cfg.Dev
	if len(args) > 0 {
		root = config.ExpandHome(args[0])
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return ui.Errorf("папки нет: %s", root)
	}
	minSize, err := diskuse.ParseSize(o.minSize)
	if err != nil {
		return err
	}
	items := par.Map(diskuse.Find(root, 6), par.Limit, func(d string) diskuse.Item {
		return diskuse.Item{Path: d, Kind: filepath.Base(d), Size: diskuse.SizeOf(d)}
	})
	if o.clean {
		return cleanStale(env.Cfg, items, minSize, o)
	}
	s := diskuse.Summarize(items, 20, minSize)
	for _, it := range s.Top {
		owner := ""
		if p, ok := env.Cfg.ProjectHere(it.Path); ok {
			owner = p.Name
		}
		ui.Println(ui.Label(ui.Pad(ui.Bytes(it.Size), 9)) + " " + ui.Pad(owner, 20) + " " + ui.Dim(paths.Show(it.Path)))
	}
	kinds := make([]string, 0, len(s.ByKind))
	for k := range s.ByKind {
		kinds = append(kinds, k)
	}
	slices.SortFunc(kinds, func(a, b string) int { return cmp.Or(cmp.Compare(s.ByKind[b], s.ByKind[a]), cmp.Compare(a, b)) })
	var parts []string
	for _, k := range kinds {
		parts = append(parts, k+" "+ui.Bytes(s.ByKind[k]))
	}
	ui.Println(ui.Info("всего "+ui.Bytes(s.Total)) + ui.Dim("  в "+strconv.Itoa(len(items))+" папках: "+strings.Join(parts, ", ")))
	ui.Println(ui.Dim("всё это пересоздаётся установкой зависимостей или сборкой; заброшенное убрать: tim disk --clean"))
	return nil
}

// cleanStale предлагает убрать в Корзину зависимости проектов, которые давно не трогали.
func cleanStale(cfg *config.Config, items []diskuse.Item, minSize int64, o options) error {
	touched := touchTimes(cfg, items)
	stale := slices.DeleteFunc(diskuse.Stale(items, touched, time.Now(), o.days, minSize), func(it diskuse.Item) bool {
		if gitx.Tracked(it.Path) {
			ui.Println(ui.Dim("пропускаю " + it.Path + ": лежит в git"))
			return true
		}
		return false
	})
	if len(stale) == 0 {
		ui.Println(ui.Dim("заброшенных дольше " + strconv.Itoa(o.days) + " дн папок крупнее " + o.minSize + " нет"))
		return nil
	}
	var freed int64
	for _, it := range stale {
		owner, _ := cfg.ProjectHere(it.Path)
		days := int(time.Since(touched[it.Path]).Hours() / 24)
		q := fmt.Sprintf("%s %s (%s, не трогали %d дн) - в Корзину?", ui.Bytes(it.Size), paths.Show(it.Path), owner.Name, days)
		if !runner.Confirm(q) {
			continue
		}
		if err := trash.Move(it.Path); err != nil {
			ui.Println(ui.Bad(err.Error()))
			continue
		}
		freed += it.Size
	}
	if freed == 0 {
		ui.Println(ui.Dim("ничего не убрано; кандидатов " + strconv.Itoa(len(stale))))
		return nil
	}
	ui.Println(ui.Info("освобождено "+ui.Bytes(freed)) + ui.Dim("  вернуть можно из Корзины"))
	return nil
}

// touchTimes: когда трогали проект каждой папки - последний коммит или изменение в папке над
// найденной, что позже. git log идёт один раз на проект, а не на каждую найденную папку.
// Папки вне реестра отметки не получают и в очистку не попадают.
func touchTimes(cfg *config.Config, items []diskuse.Item) map[string]time.Time {
	var mu sync.Mutex
	commits := map[string]func() time.Time{}
	commitOf := func(dir string) time.Time {
		mu.Lock()
		f, ok := commits[dir]
		if !ok {
			f = sync.OnceValue(func() time.Time { return gitx.LastCommit(dir) })
			commits[dir] = f
		}
		mu.Unlock()
		return f()
	}
	times := par.Map(items, par.Limit, func(it diskuse.Item) time.Time {
		p, ok := cfg.ProjectHere(it.Path)
		if !ok {
			return time.Time{}
		}
		t := commitOf(cfg.ProjectDir(p))
		if st, err := os.Stat(filepath.Dir(it.Path)); err == nil && st.ModTime().After(t) {
			t = st.ModTime()
		}
		return t
	})
	touched := map[string]time.Time{}
	for i, it := range items {
		touched[it.Path] = times[i]
	}
	return touched
}
