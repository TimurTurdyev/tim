package cmd

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	diskMin   string
	diskClean bool
	diskDays  int
	diskSys   bool
)

// engineHint: docker system prune чистит только через работающий движок - у выключенного
// OrbStack нет сокета, и команда падает с "no such file or directory".
func engineHint(name, clean string) string {
	if name != "OrbStack" || !runner.Has("orb") {
		return clean
	}
	out, _ := runner.Output("orb", "status")
	if strings.TrimSpace(out) == "Running" {
		return clean
	}
	return "OrbStack выключен: orb start, " + clean + ", orb stop"
}

// tilde: путь от домашней папки так, как его набирают в терминале.
func tilde(rel string) string { return "~/" + rel }

// placePath: где лежит место; у мест из нескольких папок - самая большая из них.
func placePath(p diskuse.Place, sizes map[string]int64) string {
	best := p.Paths[0]
	for _, path := range p.Paths {
		if sizes[path] > sizes[best] {
			best = path
		}
	}
	return tilde(best)
}

func subRow(size int64, name, path string) string {
	return "          " + ui.Pad(fmtBytes(size), 10) + ui.Pad(name, 24) + path
}

// printModels: модели Hugging Face по размеру и с тем, когда их последний раз запускали:
// без этого непонятно, какую из моделей по 8 ГБ можно убрать.
func printModels(hub string) {
	models := diskuse.HFModels(hub)
	var wg sync.WaitGroup
	for i := range models {
		wg.Go(func() {
			models[i].Size = diskuse.ModelSize(models[i].Path)
			models[i].LastUsed = diskuse.LastUsed(models[i].Path)
		})
	}
	wg.Wait()
	diskuse.SortModels(models)
	for _, m := range models {
		if m.Size < 50<<20 {
			continue
		}
		used := "не запускали"
		if !m.LastUsed.IsZero() {
			days := int(time.Since(m.LastUsed).Hours() / 24)
			switch {
			case days == 0:
				used = "запускали сегодня"
			case days < 30:
				used = "запускали " + strconv.Itoa(days) + " дн назад"
			default:
				used = ui.Warn("не запускали " + strconv.Itoa(days) + " дн")
			}
		}
		ui.Println(ui.Dim("          "+ui.Pad(fmtBytes(m.Size), 10)+ui.Pad(m.Name, 44)) + ui.Pad(used, 24) +
			ui.Dim(tilde(filepath.Join(".cache", "huggingface", "hub", filepath.Base(m.Path)))))
	}
}

// topCaches: самые большие подпапки общего кеша, кроме тех, что уже показаны отдельной строкой.
func topCaches(sizes map[string]int64, rel string, skip bool, n int) []diskuse.Item {
	var items []diskuse.Item
	for _, it := range diskuse.Children(sizes, rel) {
		if skip && shownPlace(rel+"/"+it.Path) {
			continue
		}
		items = append(items, it)
	}
	return items[:min(n, len(items))]
}

func shownPlace(rel string) bool {
	for _, p := range diskuse.SystemPlaces {
		if slices.Contains(p.Paths, rel) {
			return true
		}
	}
	return false
}

// diskSystem: куда делось место кроме проектов. Только показ и команды, ничего не удаляет.
func diskSystem() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	// Общие кеши - одним du на корень: из него же берутся размеры вложенных мест и топ прочего.
	// Отдельно меряется только то, что лежит вне них.
	roots := map[string]string{diskuse.Caches: "2", diskuse.DotCache: "1"}
	sizes := map[string]int64{}
	var mu sync.Mutex
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	measure := func(rel string, args ...string) {
		full := filepath.Join(home, rel)
		if _, err := os.Stat(full); err != nil {
			return
		}
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			// du без прав на часть подпапок пишет ошибки, но итог печатает - его и берём.
			out, _ := runner.Output("du", append(args, full)...)
			got := diskuse.ParseDu(out, home)
			mu.Lock()
			maps.Copy(sizes, got)
			mu.Unlock()
		})
	}
	for rel, depth := range roots {
		measure(rel, "-k", "-d", depth)
	}
	for _, rel := range diskuse.AllPaths(diskuse.SystemPlaces) {
		if _, root := roots[rel]; !root && !diskuse.Under(rel, diskuse.Caches, diskuse.DotCache) {
			measure(rel, "-sk")
		}
	}
	wg.Wait()
	list := diskuse.SummarizeSystem(diskuse.SystemPlaces, sizes)
	var total int64
	for _, m := range list {
		if m.Size < 50<<20 {
			continue
		}
		total += m.Size
		line := ui.Label(ui.Pad(fmtBytes(m.Size), 10)) + ui.Pad(m.Name, 28)
		hint := []string{placePath(m.Place, sizes)}
		if m.Clean != "" {
			hint = append(hint, engineHint(m.Name, m.Clean))
		}
		ui.Println(line + ui.Dim(strings.Join(hint, "  \u2022  ")))
		switch m.Paths[0] {
		case diskuse.DotCache:
			for _, sub := range topCaches(sizes, diskuse.DotCache, true, 3) {
				ui.Println(ui.Dim(subRow(sub.Size, sub.Path, tilde(filepath.Join(diskuse.DotCache, sub.Path)))))
			}
		case ".cache/huggingface":
			printModels(filepath.Join(home, ".cache", "huggingface", "hub"))
		case diskuse.Caches:
			for _, sub := range topCaches(sizes, diskuse.Caches, true, 3) {
				ui.Println(ui.Dim(subRow(sub.Size, sub.Path, tilde(filepath.Join(diskuse.Caches, sub.Path)))))
			}
		case diskuse.Caches + "/JetBrains":
			subs := topCaches(sizes, m.Paths[0], false, 20)
			names := make([]string, len(subs))
			for i, sub := range subs {
				names[i] = sub.Path
			}
			old := diskuse.OldIDEVersions(names)
			for _, sub := range subs {
				if sub.Size < 50<<20 {
					continue
				}
				rel := filepath.Join(m.Paths[0], sub.Path)
				if old[sub.Path] {
					// Готовая команда: trash кладёт в Корзину, а не стирает - передумал, вернул.
					// Тильда остаётся снаружи кавычек, иначе shell её не раскроет.
					ui.Println(ui.Dim("          "+ui.Pad(fmtBytes(sub.Size), 10)+ui.Pad(sub.Path, 24)) +
						ui.Warn("старая версия") + ui.Dim("  trash ~/"+shellq.Join([]string{rel})))
					continue
				}
				ui.Println(ui.Dim(subRow(sub.Size, sub.Path, tilde(rel))))
			}
		}
	}
	ui.Println(ui.Info("всего "+fmtBytes(total)) + ui.Dim("  места крупнее 50 MB; tim ничего не удаляет"))
	return nil
}

// lastTouched: когда трогали проект - последний коммит или изменение в папке над найденной, что позже.
// Папки вне реестра не получают отметки и в очистку не попадают. commits - время коммита по проекту,
// чтобы git log шёл один раз на проект, а не на каждую найденную папку.
func lastTouched(path string, commits func(dir string) time.Time) time.Time {
	p, ok := cfg.ProjectHere(path)
	if !ok {
		return time.Time{}
	}
	t := commits(cfg.ProjectDir(p))
	if st, err := os.Stat(filepath.Dir(path)); err == nil && st.ModTime().After(t) {
		t = st.ModTime()
	}
	return t
}

func lastCommit(dir string) time.Time {
	out, err := runner.Output("git", "-C", dir, "log", "-1", "--format=%ct")
	if err != nil {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// tracked: лежит ли в папке что-то под git - закоммиченный vendor или build пересоздать нельзя.
func tracked(path string) bool {
	out, err := runner.Output("git", "-C", filepath.Dir(path), "ls-files", "--", filepath.Base(path))
	return err == nil && strings.TrimSpace(out) != ""
}

// moveToTrash: в Корзину, откуда можно вернуть, а не rm.
func moveToTrash(path string) error {
	switch {
	case runner.Has("trash") && runtime.GOOS == "darwin":
		return runner.Run("trash", path)
	case runner.Has("gio"):
		return runner.Run("gio", "trash", path)
	}
	return ui.Errorf("не нашёл, чем класть в Корзину (trash на macOS, gio на Linux); %s не тронут", path)
}

func cleanStale(items []diskuse.Item, min int64) error {
	touched := map[string]time.Time{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	commits := map[string]func() time.Time{}
	commitOf := func(dir string) time.Time {
		mu.Lock()
		f, ok := commits[dir]
		if !ok {
			f = sync.OnceValue(func() time.Time { return lastCommit(dir) })
			commits[dir] = f
		}
		mu.Unlock()
		return f()
	}
	sem := make(chan struct{}, 8)
	for _, it := range items {
		wg.Go(func() {
			sem <- struct{}{}
			t := lastTouched(it.Path, commitOf)
			<-sem
			mu.Lock()
			touched[it.Path] = t
			mu.Unlock()
		})
	}
	wg.Wait()
	stale := slices.DeleteFunc(diskuse.Stale(items, touched, time.Now(), diskDays, min), func(it diskuse.Item) bool {
		if tracked(it.Path) {
			ui.Println(ui.Dim("пропускаю " + it.Path + ": лежит в git"))
			return true
		}
		return false
	})
	if len(stale) == 0 {
		ui.Println(ui.Dim("заброшенных дольше " + strconv.Itoa(diskDays) + " дн папок крупнее " + diskMin + " нет"))
		return nil
	}
	home := paths.Home()
	var freed int64
	for _, it := range stale {
		owner, _ := cfg.ProjectHere(it.Path)
		days := int(time.Since(touched[it.Path]).Hours() / 24)
		q := fmt.Sprintf("%s %s (%s, не трогали %d дн) - в Корзину?", fmtBytes(it.Size),
			strings.Replace(it.Path, home, "~", 1), owner.Name, days)
		if !runner.Confirm(q) {
			continue
		}
		if err := moveToTrash(it.Path); err != nil {
			ui.Println(ui.Bad(err.Error()))
			continue
		}
		freed += it.Size
	}
	if freed == 0 {
		ui.Println(ui.Dim("ничего не убрано; кандидатов " + strconv.Itoa(len(stale))))
		return nil
	}
	ui.Println(ui.Info("освобождено "+fmtBytes(freed)) + ui.Dim("  вернуть можно из Корзины"))
	return nil
}

// parseSize: 50M, 1G, 500K или байты.
func parseSize(s string) (int64, error) {
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

var diskCmd = &cobra.Command{
	Use:   "disk [папка] [--min 50M]",
	Short: "Сколько места заняли node_modules, vendor, venv и сборки по проектам; --clean убирает заброшенное",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if diskSys {
			return diskSystem()
		}
		root := cfg.Dev
		if len(args) > 0 {
			root = config.ExpandHome(args[0])
		}
		if st, err := os.Stat(root); err != nil || !st.IsDir() {
			return ui.Errorf("папки нет: %s", root)
		}
		min, err := parseSize(diskMin)
		if err != nil {
			return err
		}
		dirs := diskuse.Find(root, 6)
		items := make([]diskuse.Item, len(dirs))
		sem := make(chan struct{}, 8)
		var wg sync.WaitGroup
		for i, d := range dirs {
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				out, _ := runner.Output("du", "-sk", d)
				kb, _ := strconv.ParseInt(strings.Fields(out + " 0")[0], 10, 64)
				items[i] = diskuse.Item{Path: d, Kind: filepath.Base(d), Size: kb * 1024}
			})
		}
		wg.Wait()
		if diskClean {
			return cleanStale(items, min)
		}
		s := diskuse.Summarize(items, 20, min)
		home := paths.Home()
		for _, it := range s.Top {
			owner := ""
			if p, ok := cfg.ProjectHere(it.Path); ok {
				owner = p.Name
			}
			ui.Println(ui.Label(ui.Pad(fmtBytes(it.Size), 9)) + " " + ui.Pad(owner, 20) + " " + ui.Dim(strings.Replace(it.Path, home, "~", 1)))
		}
		kinds := make([]string, 0, len(s.ByKind))
		for k := range s.ByKind {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return s.ByKind[kinds[i]] > s.ByKind[kinds[j]] })
		var parts []string
		for _, k := range kinds {
			parts = append(parts, k+" "+fmtBytes(s.ByKind[k]))
		}
		ui.Println(ui.Info("всего "+fmtBytes(s.Total)) + ui.Dim("  в "+strconv.Itoa(len(items))+" папках: "+strings.Join(parts, ", ")))
		ui.Println(ui.Dim("всё это пересоздаётся установкой зависимостей или сборкой; заброшенное убрать: tim disk --clean"))
		return nil
	},
}

func init() {
	diskCmd.Flags().StringVar(&diskMin, "min", "50M", "не показывать папки меньше")
	diskCmd.Flags().BoolVar(&diskClean, "clean", false, "предложить убрать в Корзину папки заброшенных проектов")
	diskCmd.Flags().IntVar(&diskDays, "days", 60, "сколько дней без изменений считать заброшенным")
	diskCmd.Flags().BoolVar(&diskSys, "system", false, "куда делось место кроме проектов: кеши, образы, Корзина")
	root.AddCommand(diskCmd)
}
