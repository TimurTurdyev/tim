package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/brew"
	"github.com/TimurTurdyev/tim/internal/phpver"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

type phpInstall struct {
	Version phpver.Version
	Bin     string
}

// phpInstalls: версии PHP из brew; у формулы php версия спрашивается у самого бинарника.
func phpInstalls() []phpInstall {
	prefix := brew.Prefix()
	var out []phpInstall
	for _, f := range brew.Formulas() {
		if f != "php" && !strings.HasPrefix(f, "php@") {
			continue
		}
		bin := filepath.Join(prefix, "opt", f, "bin")
		var v phpver.Version
		var ok bool
		if f == "php" {
			s, _ := runner.Output(filepath.Join(bin, "php"), "-r", "echo PHP_VERSION;")
			v, ok = phpver.Parse(s)
		} else {
			v, ok = phpver.Parse(strings.TrimPrefix(f, "php@"))
		}
		if ok {
			out = append(out, phpInstall{v, bin})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[j].Version.Less(out[i].Version) })
	return out
}

func findPHP(want string) (phpInstall, error) {
	w, ok := phpver.Parse(want)
	if !ok {
		return phpInstall{}, ui.Errorf("версия вида 8.3: %s", want)
	}
	for _, i := range phpInstalls() {
		if i.Version.Major == w.Major && i.Version.Minor == w.Minor {
			return i, nil
		}
	}
	return phpInstall{}, ui.Errorf("php %s не установлен: brew install php@%s", w, w)
}

// phpPath: PATH без каталогов других версий brew php и с нужной версией первой.
func phpPath(bin string) string {
	keep := []string{bin, filepath.Join(filepath.Dir(bin), "sbin")}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.Contains(d, "/opt/php") || strings.Contains(d, "/Cellar/php") {
			continue
		}
		keep = append(keep, d)
	}
	return strings.Join(keep, string(os.PathListSeparator))
}

var phpCmd = &cobra.Command{
	Use:   "php",
	Short: "Версии PHP: установленные, активная, что требует проект",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := needBrew(); err != nil {
			return err
		}
		active, _ := runner.Output("php", "-r", "echo PHP_VERSION;")
		installs := phpInstalls()
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
		i, err := findPHP(args[0])
		if err != nil {
			return err
		}
		cwd, _ := os.Getwd()
		return runner.ExecIn(cwd, []string{"PATH=" + phpPath(i.Bin)}, args[1], args[2:]...)
	},
}

var phpUsePrint bool

var phpUseCmd = &cobra.Command{
	Use:   "use <версия>",
	Short: "Переключить PHP в текущей оболочке (через шим tim init zsh)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		i, err := findPHP(args[0])
		if err != nil {
			return err
		}
		line := fmt.Sprintf("export PATH=%q", phpPath(i.Bin))
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
