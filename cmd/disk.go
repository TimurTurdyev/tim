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
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	diskMin   string
	diskClean bool
	diskDays  int
)

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
	root.AddCommand(diskCmd)
}
