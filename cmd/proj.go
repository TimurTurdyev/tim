package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/visits"
)

var projCmd = &cobra.Command{
	Use:   "proj",
	Short: "Реестр проектов",
}

var projListCmd = &cobra.Command{
	Use:   "list [группа]",
	Short: "Список проектов, можно по началу имени или пути",
	RunE: func(cmd *cobra.Command, args []string) error {
		group := ""
		if len(args) > 0 {
			group = args[0]
		}
		ui.Println(ui.Dim(ui.Pad("ИМЯ", 24) + " " + ui.Pad("ПУТЬ", 42) + " " + ui.Pad("ПОРТ", 6) + " " + ui.Pad("БАЗА", 22) + " ДЕПЛОЙ"))
		for _, p := range cfg.Projects {
			if group != "" && !strings.HasPrefix(p.Path, group+"/") && !strings.HasPrefix(p.Name, group) {
				continue
			}
			port := "-"
			if p.Port > 0 {
				port = fmt.Sprint(p.Port)
			}
			ui.Println(ui.Label(ui.Pad(p.Name, 24)) + " " + ui.Pad(p.Path, 42) + " " + ui.Pad(port, 6) + " " + ui.Pad(ui.Dash(p.DB), 22) + " " + ui.Dim(ui.Dash(p.Deploy)))
		}
		return nil
	},
}

var projInfoCmd = &cobra.Command{
	Use:               "info [имя]",
	Short:             "Путь, стек, ssh, деплой, порт, база, ветка",
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := projectFor(first(args))
		if err != nil {
			return err
		}
		dir := cfg.ProjectDir(p)
		ui.Println(ui.Label(p.Name) + "  " + ui.Dim(run.Stack(dir)))
		ui.Println(ui.Label("путь     ") + dir)
		ui.Println(ui.Label("ssh      ") + ui.Dash(p.SSH))
		if p.Remote != "" {
			ui.Println(ui.Label("удалённо ") + p.Remote)
		}
		ui.Println(ui.Label("деплой   ") + ui.Dash(p.Deploy))
		port := "-"
		if p.Port > 0 {
			port = fmt.Sprint(p.Port)
		}
		ui.Println(ui.Label("порт     ") + port + "   " + ui.Label("база ") + ui.Dash(p.DB))
		if branch, _ := runner.Output("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "" {
			status, _ := runner.Output("git", "-C", dir, "status", "--porcelain")
			changes := strings.Count(status, "\n")
			line := ui.Label("git      ") + strings.TrimSpace(branch)
			if changes > 0 {
				line += "  " + ui.Warn(fmt.Sprintf("%d changes", changes))
			}
			ui.Println(line)
		}
		return nil
	},
}

var projHereCmd = &cobra.Command{
	Use:   "here",
	Short: "Имя проекта для текущей папки",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		p, ok := cfg.ProjectHere(cwd)
		if !ok {
			return ui.Errorf("не в проекте из конфига")
		}
		ui.Println(p.Name)
		return nil
	},
}

var goPrint bool

var goCmd = &cobra.Command{
	Use:               "go [имя] [.]",
	Short:             "Перейти в проект; с точкой в папку над клоном, если задан wrapper",
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(cfg.Projects) == 0 {
			return ui.Errorf("реестр пуст: tim config init найдёт клоны и предложит добавить")
		}
		if len(args) == 0 && !tui.Available() {
			// Шим делает cd в то, что напечатано в stdout: список туда попасть не должен.
			if goPrint {
				ui.UseStderr()
				_ = projListCmd.RunE(cmd, nil)
				return ui.Errorf("выбрать без терминала нельзя: tim go <имя>")
			}
			return projListCmd.RunE(cmd, nil)
		}
		var p *config.Project
		if len(args) == 0 {
			picked, err := pickProject()
			if err != nil {
				return err
			}
			p = picked
		} else {
			found, ok := cfg.Project(args[0])
			if !ok {
				return ui.Errorf("нет проекта %s: tim proj list", args[0])
			}
			p = found
		}
		dir := cfg.ProjectDir(p)
		if len(args) > 1 && args[1] == "." && cfg.Wrapper != "" && filepath.Base(dir) == cfg.Wrapper {
			dir = filepath.Dir(dir)
		}
		if _, err := os.Stat(dir); err != nil {
			return ui.Errorf("папки нет: %s", dir)
		}
		visits.Record(p.Name, time.Now())
		if goPrint {
			fmt.Println(dir)
			return nil
		}
		ui.Println(dir)
		ui.Println(ui.Dim(shimHint()))
		return nil
	},
}

// shimHint: почему cd не сработал - шим не подключён или оболочка открыта до подключения.
func shimHint() string {
	path, _ := rcFile("")
	if sourced(path, 2) {
		return "эта оболочка открыта раньше подключения tim: открой новый терминал, и cd заработает"
	}
	return "чтобы cd работал из команды, выполни: tim init"
}

func first(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func init() {
	goCmd.Flags().BoolVar(&goPrint, "print", false, "только напечатать путь (для шима)")
	projCmd.AddCommand(projListCmd, projInfoCmd, projHereCmd)
	root.AddCommand(projCmd, goCmd)
}
