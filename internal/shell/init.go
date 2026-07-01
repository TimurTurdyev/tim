package shell

import (
	"fmt"
	"os"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/rcfile"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

func printScript(env *app.Env, shell string) error {
	s, err := rcfile.Script(shell, env.Cfg.Shell)
	if err != nil {
		return err
	}
	fmt.Print(s)
	return nil
}

// setupShell дописывает подключение tim в файл настроек оболочки, если его там ещё нет.
func setupShell(shell string) error {
	path, name := rcfile.Path(shell)
	if rcfile.Sourced(path, 2) {
		ui.Println(ui.Label("уже подключено ") + ui.Dim(path))
	} else {
		if err := rcfile.Append(path, `eval "$(tim init `+name+`)"`); err != nil {
			return err
		}
		if !runner.DryRun {
			ui.Println(ui.Label("подключено ") + path)
		}
	}
	if _, err := os.Stat(config.Path()); err != nil {
		ui.Println(ui.Warn("конфига нет") + ui.Dim("  собрать реестр проектов: tim config init"))
	} else {
		ui.Println(ui.Dim("конфиг на месте: " + config.Path()))
	}
	if os.Getenv("TIM_SHIM") == "" {
		ui.Println(ui.Dim("открой новый терминал, чтобы заработали tim go и tim php use"))
	}
	return nil
}
