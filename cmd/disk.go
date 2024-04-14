package cmd

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var diskMin string

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
	Short: "Сколько места заняли node_modules, vendor, venv и сборки по проектам (только показ)",
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
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				out, _ := runner.Output("du", "-sk", d)
				kb, _ := strconv.ParseInt(strings.Fields(out + " 0")[0], 10, 64)
				items[i] = diskuse.Item{Path: d, Kind: filepath.Base(d), Size: kb * 1024}
			})
		}
		wg.Wait()
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
		ui.Println(ui.Dim("всё это пересоздаётся установкой зависимостей или сборкой; tim ничего не удаляет"))
		return nil
	},
}

func init() {
	diskCmd.Flags().StringVar(&diskMin, "min", "50M", "не показывать папки меньше")
	root.AddCommand(diskCmd)
}
