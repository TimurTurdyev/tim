package cmd

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/scan"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const scanDepth = 4

// newClones: клоны под корнем, которых нет в реестре, с предложенными именами.
func scanClones(root string) ([]string, error) { return scan.Clones(root, scanDepth) }

func newClones(c *config.Config) ([]config.Project, error) {
	clones, err := scanClones(c.Dev)
	if err != nil {
		return nil, ui.Errorf("поиск в %s: %v", c.Dev, err)
	}
	known := map[string]bool{}
	taken := map[string]bool{}
	for i := range c.Projects {
		known[filepath.Clean(c.ProjectDir(&c.Projects[i]))] = true
		taken[c.Projects[i].Name] = true
	}
	var fresh []string
	for _, cl := range clones {
		if !known[filepath.Clean(cl)] {
			fresh = append(fresh, cl)
		}
	}
	names := scan.Names(fresh, c.Wrapper, taken)
	out := make([]config.Project, len(fresh))
	for i, cl := range fresh {
		out[i] = config.Project{Name: names[i], Path: c.Relative(cl)}
	}
	return out, nil
}

// chooseProjects: all - все найденные; иначе мультивыбор при tty (всё отмечено), без tty - ничего.
func chooseProjects(found []config.Project, all bool) ([]config.Project, error) {
	if all {
		return found, nil
	}
	if !tui.Available() {
		return nil, nil
	}
	items := make([]tui.Item, len(found))
	sel := make([]bool, len(found))
	for i, p := range found {
		items[i] = tui.Item{Title: p.Name, Hint: p.Path}
		sel[i] = true
	}
	idx, err := tui.MultiSelect("Какие проекты добавить в реестр?", items, sel)
	if err != nil {
		return nil, pickErr(err)
	}
	out := make([]config.Project, 0, len(idx))
	for _, i := range idx {
		out = append(out, found[i])
	}
	return out, nil
}

func addAll(c *config.Config, list []config.Project) int {
	n := 0
	for _, p := range list {
		if c.AddProject(p) == nil {
			n++
		}
	}
	return n
}

var scanAdd bool

var projScanCmd = &cobra.Command{
	Use:   "scan [--add]",
	Short: "Найти git-клоны под корнем, которых нет в реестре",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		found, err := newClones(cfg)
		if err != nil {
			return err
		}
		if len(found) == 0 {
			ui.Println(ui.Dim("новых клонов в " + cfg.Dev + " нет"))
			return nil
		}
		for _, p := range found {
			ui.Println(ui.Label(ui.Pad(p.Name, 24)) + " " + p.Path)
		}
		if !scanAdd && !tui.Available() {
			ui.Println(ui.Dim("добавить: tim proj scan --add"))
			return nil
		}
		chosen, err := chooseProjects(found, scanAdd)
		if err != nil || len(chosen) == 0 {
			return err
		}
		if scanAdd && !runner.Confirm("добавить в реестр "+strconv.Itoa(len(chosen))+"?") {
			return nil
		}
		n := addAll(cfg, chosen)
		if err := saveConfig(cfg); err != nil {
			return err
		}
		ui.Println(ui.Label("добавлено ") + strconv.Itoa(n))
		return nil
	},
}

var (
	addName string
	addURL  string
)

var projAddCmd = &cobra.Command{
	Use:   "add [путь] [--name имя] [--url адрес]",
	Short: "Добавить папку в реестр (по умолчанию текущую)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, _ := os.Getwd()
		if len(args) > 0 {
			dir = args[0]
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			return ui.Errorf("папки нет: %s", abs)
		}
		for i := range cfg.Projects {
			if filepath.Clean(cfg.ProjectDir(&cfg.Projects[i])) == abs {
				return ui.Errorf("%s уже в реестре как %s", abs, cfg.Projects[i].Name)
			}
		}
		name := addName
		if name == "" {
			taken := map[string]bool{}
			for _, p := range cfg.Projects {
				taken[p.Name] = true
			}
			name = scan.Names([]string{abs}, cfg.Wrapper, taken)[0]
		}
		if err := cfg.AddProject(config.Project{Name: name, Path: cfg.Relative(abs), URL: addURL}); err != nil {
			return err
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		ui.Println(ui.Label("добавлен ") + name + "  " + ui.Dim(cfg.Relative(abs)))
		return nil
	},
}

var projRmCmd = &cobra.Command{
	Use:               "rm <имя>",
	Short:             "Убрать проект из реестра (папка остаётся)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, ok := cfg.Project(args[0]); !ok {
			return ui.Errorf("нет проекта %s в реестре", args[0])
		}
		if !runner.Confirm("убрать " + args[0] + " из реестра?") {
			return nil
		}
		cfg.RemoveProject(args[0])
		if err := saveConfig(cfg); err != nil {
			return err
		}
		ui.Println(ui.Label("убран ") + args[0])
		return nil
	},
}

func init() {
	projScanCmd.Flags().BoolVar(&scanAdd, "add", false, "добавить все найденные")
	projAddCmd.Flags().StringVar(&addName, "name", "", "имя в реестре")
	projAddCmd.Flags().StringVar(&addURL, "url", "", "адрес сайта проекта (для tim ssl)")
	projCmd.AddCommand(projScanCmd, projAddCmd, projRmCmd)
}
