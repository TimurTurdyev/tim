package cmd

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// saveConfig пишет конфиг; в dry-run только сообщает, что записал бы.
func saveConfig(c *config.Config) error {
	if runner.DryRun {
		ui.Println(ui.Dim("dry-run: конфиг " + config.Path() + " не записан"))
		return nil
	}
	lost := config.HasComments()
	if err := config.Save(c); err != nil {
		return err
	}
	if lost {
		ui.Println(ui.Warn("комментарии из конфига при записи не сохраняются; прежний файл: " + config.Path() + ".bak"))
	}
	return nil
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Где лежит конфиг и что в нём",
	RunE: func(cmd *cobra.Command, args []string) error {
		ui.Println(ui.Dim(config.Path()))
		return toml.NewEncoder(os.Stdout).Encode(cfg)
	},
}

var (
	initRoot    string
	initWrapper string
	initAll     bool
	initYes     bool
	initMerge   bool
)

var configInitCmd = &cobra.Command{
	Use:   "init [--root папка] [--wrapper app] [--all] [--merge] [-y]",
	Short: "Мастер первого конфига: корень проектов, поиск клонов, реестр",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, statErr := os.Stat(config.Path())
		exists := statErr == nil
		c := config.Default()
		switch {
		case exists && initMerge:
			c = cfg
		case exists && !initYes:
			if !runner.Confirm("конфиг " + config.Path() + " уже есть, перезаписать? (дополнить: --merge)") {
				return nil
			}
		}
		interactive := tui.Available() && !initYes
		root, err := askRoot(c.Dev, interactive)
		if err != nil {
			return err
		}
		c.Dev = root
		switch {
		case cmd.Flags().Changed("wrapper"):
			c.Wrapper = initWrapper
		case !interactive && c.Wrapper == "":
			c.Wrapper = guessWrapper(root)
		case interactive:
			guess := c.Wrapper
			if guess == "" {
				guess = guessWrapper(root)
			}
			w, err := tui.Input("Подпапка с клоном внутри папки проекта (пусто, если клон лежит прямо в ней)", guess)
			if err != nil {
				return pickErr(err, "--wrapper", nil)
			}
			c.Wrapper = w
		}
		found, err := newClones(c)
		if err != nil {
			return err
		}
		ui.Println(ui.Label("корень ") + c.Dev + ui.Dim("  найдено новых клонов: "+strconv.Itoa(len(found))))
		chosen := found
		if !initAll {
			if chosen, err = chooseProjects(found, initYes); err != nil {
				return err
			}
		}
		n := addAll(c, chosen)
		if err := saveConfig(c); err != nil {
			return err
		}
		ui.Println(ui.Label("записан ") + config.Path() + ui.Dim("  проектов в реестре: "+strconv.Itoa(len(c.Projects))+", добавлено "+strconv.Itoa(n)))
		ui.Println(ui.Dim("дальше: добавь в ~/.zshrc строку eval \"$(tim init zsh)\" и попробуй tim go"))
		return nil
	},
}

func askRoot(current string, interactive bool) (string, error) {
	root := current
	if initRoot != "" {
		root = initRoot
	} else if interactive {
		r, err := tui.Input("Корень, под которым лежат проекты", current)
		if err != nil {
			return "", pickErr(err, "--root", nil)
		}
		root = r
	}
	root = config.ExpandHome(root)
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return "", ui.Errorf("папки нет: %s", abs)
	}
	return abs, nil
}

// guessWrapper: если у нескольких клонов одинаковое имя папки (app, src), это скорее всего обёртка.
func guessWrapper(root string) string {
	clones, err := scanClones(root)
	if err != nil {
		return ""
	}
	count := map[string]int{}
	for _, c := range clones {
		count[filepath.Base(c)]++
	}
	best, bestN := "", 1
	for name, n := range count {
		if n > bestN {
			best, bestN = name, n
		}
	}
	return best
}

var configMigrateCmd = &cobra.Command{
	Use:    "migrate <папка с tools.projects, tools.hosts, tools.conf>",
	Short:  "Собрать конфиг из файлов zsh-версии tools",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := config.Migrate(args[0])
		if err != nil {
			return err
		}
		if _, err := os.Stat(config.Path()); err == nil {
			if !runner.Confirm("конфиг " + config.Path() + " уже есть, перезаписать?") {
				return nil
			}
		}
		if err := saveConfig(c); err != nil {
			return err
		}
		ui.Printf("%s %s: проектов %d, хостов %d, туннель %s\n", ui.Label("записан"), config.Path(), len(c.Projects), len(c.Hosts), c.Tunnel.Host)
		return nil
	},
}

func init() {
	f := configInitCmd.Flags()
	f.StringVar(&initRoot, "root", "", "корень проектов")
	f.StringVar(&initWrapper, "wrapper", "", "подпапка с клоном внутри папки проекта, например app")
	f.BoolVar(&initAll, "all", false, "добавить все найденные клоны")
	f.BoolVarP(&initYes, "yes", "y", false, "без вопросов")
	f.BoolVar(&initMerge, "merge", false, "дополнить существующий конфиг")
	configCmd.AddCommand(configInitCmd, configMigrateCmd)
	root.AddCommand(configCmd)
}
