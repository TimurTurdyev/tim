// Package cmd: команды tim на cobra.
package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var cfg *config.Config

var root = &cobra.Command{
	Use:   "tim",
	Short: "Пульт разработчика: проекты, серверы, маршруты, туннели, базы",
	Long: `tim держит под рукой всё хозяйство ~/Dev: реестр проектов, ssh с обходом VPN,
маршруты, туннели через свой сервер, базы по .env проекта.
TIM_DRY_RUN=1 или --dry-run печатает команды, меняющие систему, вместо запуска.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return ui.Errorf("конфиг %s: %v", config.Path(), err)
		}
		return nil
	},
}

func init() {
	root.PersistentFlags().BoolVar(&runner.DryRun, "dry-run", runner.DryRun, "показать команды, ничего не выполняя")
	root.CompletionOptions.HiddenDefaultCmd = true
	runner.ConfirmFunc = func(q string) (bool, bool) {
		if !tui.Available() {
			return false, false
		}
		yes, err := tui.Confirm(q)
		if err != nil && !errors.Is(err, tui.ErrCanceled) {
			return false, false
		}
		return yes, true
	}
}

var groups = []struct {
	id, title string
	cmds      []string
}{
	{"proj", "Проекты:", []string{"proj", "go", "db", "config", "init"}},
	{"net", "Сервер и сеть:", []string{"ssh", "route", "cert", "dns", "port", "site"}},
	{"util", "Утилиты:", []string{"hex", "dec", "b64", "jwt", "ts", "uuid", "hash", "json", "version"}},
}

// assignGroups раскладывает команды по разделам справки; вызывается после всех init().
func assignGroups() {
	byName := map[string]*cobra.Command{}
	for _, c := range root.Commands() {
		byName[c.Name()] = c
	}
	for _, g := range groups {
		root.AddGroup(&cobra.Group{ID: g.id, Title: g.title})
		for _, n := range g.cmds {
			if c, ok := byName[n]; ok {
				c.GroupID = g.id
			}
		}
	}
}

func Execute() error {
	assignGroups()
	if err := root.Execute(); err != nil {
		if errors.Is(err, errPickCanceled) {
			return nil
		}
		ui.Println(ui.Bad(err.Error()))
		return err
	}
	return nil
}

func projectNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	c, err := config.Load()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}
	var names []string
	for _, p := range c.Projects {
		names = append(names, p.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}
