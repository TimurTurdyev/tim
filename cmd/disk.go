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

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/diskuse"
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

// topCaches: самые большие подпапки общего кеша, кроме тех, что уже показаны отдельной строкой.
func topCaches(dir string, known map[string]int64, n int) []diskuse.Item {
	if known == nil {
		known = map[string]int64{}
	}
	out, _ := runner.Output("sh", "-c", "du -sk "+shellq.Quote(dir)+"/* 2>/dev/null")
	var items []diskuse.Item
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, "\t", 2)
		if len(f) != 2 {
			continue
		}
		name := filepath.Base(f[1])
		if _, shown := known[diskuse.Caches+"/"+name]; shown {
			continue
		}
		kb, _ := strconv.ParseInt(f[0], 10, 64)
		items = append(items, diskuse.Item{Path: name, Size: kb * 1024})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Size > items[j].Size })
	return items[:min(n, len(items))]
}

// diskSystem: куда делось место кроме проектов. Только показ и команды, ничего не удаляет.
func diskSystem() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	paths := diskuse.AllPaths(diskuse.SystemPlaces)
	sizes := map[string]int64{}
	var mu sync.Mutex
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for _, rel := range paths {
		full := filepath.Join(home, rel)
		if _, err := os.Stat(full); err != nil {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			// du без прав на часть подпапок пишет ошибки, но итог печатает - его и берём.
			out, _ := runner.Output("du", "-sk", full)
			kb, _ := strconv.ParseInt(strings.Fields(out + " 0")[0], 10, 64)
			mu.Lock()
			sizes[rel] = kb * 1024
			mu.Unlock()
		}()
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
		if m.Clean != "" {
			line += ui.Dim(m.Clean)
		}
		ui.Println(line)
		switch m.Paths[0] {
		case diskuse.Caches:
			for _, sub := range topCaches(filepath.Join(home, diskuse.Caches), sizes, 3) {
				ui.Println(ui.Dim("          " + ui.Pad(fmtBytes(sub.Size), 10) + sub.Path))
			}
		case diskuse.Caches + "/JetBrains":
			subs := topCaches(filepath.Join(home, m.Paths[0]), nil, 20)
			names := make([]string, len(subs))
			for i, sub := range subs {
				names[i] = sub.Path
			}
			old := diskuse.OldIDEVersions(names)
			for _, sub := range subs {
				if sub.Size < 50<<20 {
					continue
				}
				line := "          " + ui.Pad(fmtBytes(sub.Size), 10) + ui.Pad(sub.Path, 18)
				if old[sub.Path] {
					ui.Println(ui.Dim(line) + ui.Warn("старая версия, можно удалить"))
					continue
				}
				ui.Println(ui.Dim(line))
			}
		}
	}
	ui.Println(ui.Info("всего "+fmtBytes(total)) + ui.Dim("  места крупнее 50 MB; tim ничего не удаляет"))
	return nil
}

// lastTouched: когда трогали проект - последний коммит или изменение в папке над найденной, что позже.
// Папки вне реестра не получают отметки и в очистку не попадают.
func lastTouched(path string) time.Time {
	p, ok := cfg.ProjectHere(path)
	if !ok {
		return time.Time{}
	}
	var t time.Time
	if out, err := runner.Output("git", "-C", cfg.ProjectDir(p), "log", "-1", "--format=%ct"); err == nil {
		if sec, err := strconv.ParseInt(strings.TrimSpace(out), 10, 64); err == nil {
			t = time.Unix(sec, 0)
		}
	}
	if st, err := os.Stat(filepath.Dir(path)); err == nil && st.ModTime().After(t) {
		t = st.ModTime()
	}
	return t
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
	for _, it := range items {
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := lastTouched(it.Path)
			mu.Lock()
			touched[it.Path] = t
			mu.Unlock()
		}()
	}
	wg.Wait()
	stale := diskuse.Stale(items, touched, time.Now(), diskDays, min)
	if len(stale) == 0 {
		ui.Println(ui.Dim("заброшенных дольше " + strconv.Itoa(diskDays) + " дн папок крупнее " + diskMin + " нет"))
		return nil
	}
	home, _ := os.UserHomeDir()
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
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				out, _ := runner.Output("du", "-sk", d)
				kb, _ := strconv.ParseInt(strings.Fields(out + " 0")[0], 10, 64)
				items[i] = diskuse.Item{Path: d, Kind: filepath.Base(d), Size: kb * 1024}
			}()
		}
		wg.Wait()
		if diskClean {
			return cleanStale(items, min)
		}
		s := diskuse.Summarize(items, 20, min)
		home, _ := os.UserHomeDir()
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
