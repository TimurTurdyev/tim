// Package app: то, что корень tim передаёт разделам, - конфиг и общие для всех разделов действия.
package app

import (
	"strings"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Env: окружение команд. Корень заполняет Cfg перед запуском команды, остальное - при сборке.
// Раздел получает указатель в Commands(env) и передаёт нужное дальше аргументами.
type Env struct {
	Cfg     *config.Config
	Version string

	// Экранам - запуск других команд tim, о которых раздел не знает: показать результат
	// не выходя из экрана, выполнить после закрытия экрана, открыть экран на весь терминал.
	Result func(args []string) tui.Action
	After  func(args []string)
	Open   func(s tui.Screen) error
	// Inside: команда tim внутри экрана - вывод собирается, вопросы не задаются; для работы в фоне.
	Inside func(args []string) (string, error)
}

// Build: сборка для лок-апа - "сборка e3642b7" до первого тега, у релиза его версия. Версия go install
// вида v0.0.0-20251022162241-8c6e20c7cc4a+dirty длинная и ничего не говорит человеку.
func (e *Env) Build() string {
	v := e.Version
	if i := strings.LastIndex(v, "-"); i > 0 && strings.HasPrefix(v, "v0.0.0-") {
		c := strings.TrimSuffix(v[i+1:], "+dirty")
		if len(c) > 7 {
			c = c[:7]
		}
		return "сборка " + c
	}
	if !strings.HasPrefix(v, "v") {
		return "сборка " + v // коммит из make до первого тега или dev
	}
	return v
}

// Save записывает конфиг. В dry-run только говорит об этом; комментарии TOML при записи теряются,
// и об этом стоит предупредить один раз - прежний файл остаётся в .bak.
func (e *Env) Save() error {
	if runner.DryRun {
		ui.Println(ui.Dim("dry-run: конфиг " + config.Path() + " не записан"))
		return nil
	}
	lost := config.HasComments()
	if err := config.Save(e.Cfg); err != nil {
		return err
	}
	if lost {
		ui.Println(ui.Warn("комментарии из конфига при записи не сохраняются; прежний файл: " + config.Path() + ".bak"))
	}
	return nil
}

// First: первый аргумент или пусто - для команд с необязательным аргументом.
func First(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}
