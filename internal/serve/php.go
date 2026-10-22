package serve

import (
	"fmt"
	"os"

	"github.com/TimurTurdyev/tim/internal/phpver"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// execPHP: команда с нужной версией PHP первой в PATH.
func execPHP(args []string) error {
	if len(args) < 2 {
		return ui.Errorf("нужны версия и команда: tim php exec 8.3 composer install")
	}
	i, err := phpver.Find(args[0])
	if err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	return runner.ExecIn(cwd, []string{"PATH=" + i.PathEnv()}, args[1], args[2:]...)
}

// usePHP: процесс не меняет PATH своей оболочки, поэтому печатает export для шима tim init.
func usePHP(version string, printOnly bool) error {
	i, err := phpver.Find(version)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("export PATH=%q", i.PathEnv())
	if printOnly {
		fmt.Println(line)
		return nil
	}
	ui.Println(ui.Dim("процесс не меняет PATH своей оболочки; с шимом tim init zsh команда сработает сама, без него:"))
	fmt.Println("eval \"$(tim php use --print " + version + ")\"")
	return nil
}
