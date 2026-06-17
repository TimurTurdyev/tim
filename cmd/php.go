package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/phpver"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var phpCmd = &cobra.Command{
	Use:   "php",
	Short: "Версии PHP: установленные, активная, что требует проект",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS == "darwin" {
			if err := needBrew(); err != nil {
				return err
			}
		}
		active, _ := runner.Output("php", "-r", "echo PHP_VERSION;")
		installs := phpver.Installs()
		var versions []phpver.Version
		for _, i := range installs {
			mark := "  "
			if strings.HasPrefix(active, i.Version.String()+".") {
				mark = ui.Label("* ")
			}
			ui.Println(mark + ui.Pad(i.Version.String(), 6) + " " + ui.Dim(i.Bin))
			versions = append(versions, i.Version)
		}
		if active != "" {
			ui.Println(ui.Dim("активная " + active))
		}
		if p, err := cfg.Pick(""); err == nil {
			req := phpver.Required(filepath.Join(cfg.ProjectDir(p), "composer.json"))
			if req != "" {
				var fit []string
				for _, v := range phpver.Matching(versions, req) {
					fit = append(fit, v.String())
				}
				line := ui.Label(p.Name) + " требует php " + req
				if len(fit) == 0 {
					line += "  " + ui.Bad("подходящей версии нет")
				} else {
					line += ui.Dim("  подходят: " + strings.Join(fit, " "))
				}
				ui.Println(line)
			}
		}
		ui.Println(ui.Dim("tim php use 8.3 - в этой оболочке; tim php exec 8.3 <команда> - один раз"))
		return nil
	},
}

var phpExecCmd = &cobra.Command{
	Use:                "exec <версия> <команда...>",
	Short:              "Выполнить команду с нужной версией PHP первой в PATH",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) < 2 {
			return ui.Errorf("tim php exec 8.3 composer install")
		}
		i, err := phpver.Find(args[0])
		if err != nil {
			return err
		}
		cwd, _ := os.Getwd()
		return runner.ExecIn(cwd, []string{"PATH=" + i.PathEnv()}, args[1], args[2:]...)
	},
}

var phpUsePrint bool

var phpUseCmd = &cobra.Command{
	Use:   "use <версия>",
	Short: "Переключить PHP в текущей оболочке (через шим tim init zsh)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		i, err := phpver.Find(args[0])
		if err != nil {
			return err
		}
		line := fmt.Sprintf("export PATH=%q", i.PathEnv())
		if phpUsePrint {
			fmt.Println(line)
			return nil
		}
		ui.Println(ui.Dim("процесс не меняет PATH своей оболочки; с шимом tim init zsh команда сработает сама, без него:"))
		fmt.Println("eval \"$(tim php use --print " + args[0] + ")\"")
		return nil
	},
}

func init() {
	phpUseCmd.Flags().BoolVar(&phpUsePrint, "print", false, "только напечатать export (для шима)")
	phpCmd.AddCommand(phpExecCmd, phpUseCmd)
	root.AddCommand(phpCmd)
}
