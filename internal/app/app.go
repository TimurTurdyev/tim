// Package app: то, что корень tim передаёт разделам, - конфиг и общие для всех разделов действия.
package app

import (
	"os"
	"runtime"
	"strings"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/run"
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
	// не выходя из экрана, выполнить после закрытия экрана, открыть экран на весь терминал,
	// главный экран.
	Result func(args []string) tui.Action
	After  func(args []string)
	Open   func(s tui.Screen) error
	Home   func() tui.Screen // главный экран: туда уходит мастер первого запуска
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
	if v == "" || v == "dev" {
		return "сборка dev"
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

// Project: проект по имени, а без имени - по текущей папке; если и так не понять, спрашивает выбором.
func (e *Env) Project(name string) (*config.Project, error) {
	p, err := e.Cfg.Pick(name)
	if err != nil && name == "" && len(e.Cfg.Projects) > 0 && tui.Available() {
		return e.PickProject()
	}
	return p, err
}

// First: первый аргумент или пусто - для команд с необязательным именем проекта.
func First(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

// Projects: проекты по именам, группам @имя и частям имён; без аргументов - проект текущей папки
// или выбор. Часть имени, подходящая к нескольким, спрашивается выбором.
func (e *Env) Projects(args []string) ([]*config.Project, error) {
	if len(args) == 0 {
		p, err := e.Project("")
		if err != nil {
			return nil, err
		}
		return []*config.Project{p}, nil
	}
	var out []*config.Project
	for _, a := range args {
		found, err := e.projectsBy(a)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

func (e *Env) projectsBy(arg string) ([]*config.Project, error) {
	cfg := e.Cfg
	if g, ok := strings.CutPrefix(arg, "@"); ok {
		members, ok := cfg.Groups[g]
		if !ok {
			return nil, ui.Errorf("нет группы %s в [groups] конфига", g)
		}
		var out []*config.Project
		for _, m := range members {
			p, ok := cfg.Project(m)
			if !ok {
				return nil, ui.Errorf("в группе %s нет проекта %s в реестре", g, m)
			}
			out = append(out, p)
		}
		return out, nil
	}
	names := make([]string, len(cfg.Projects))
	for i, p := range cfg.Projects {
		names[i] = p.Name
	}
	idx := run.MatchNames(names, arg)
	switch {
	case len(idx) == 1:
		return []*config.Project{&cfg.Projects[idx[0]]}, nil
	case len(idx) == 0:
		return nil, ui.Errorf("нет проекта %s: tim proj list", arg)
	case !Interactive():
		var list []string
		for _, i := range idx {
			list = append(list, names[i])
		}
		return nil, ui.Errorf("%s подходит к нескольким: %s", arg, strings.Join(list, " "))
	}
	opts := make([]Option, len(idx))
	for k, i := range idx {
		opts[k] = Option{Title: names[i], Hint: cfg.Projects[i].Path}
	}
	k, err := Choose(arg, "проект", opts)
	if err != nil {
		return nil, err
	}
	return []*config.Project{&cfg.Projects[idx[k]]}, nil
}

// Edit открывает папку в редакторе: ключ editor конфига, $VISUAL, на Linux ещё $EDITOR; без них - Finder.
func (e *Env) Edit(dir string) error {
	editor := e.Cfg.Editor
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" && runtime.GOOS != "darwin" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return runner.OpenBrowser(dir)
	}
	parts := strings.Fields(editor)
	return runner.Run(parts[0], append(parts[1:], dir)...)
}

// Named: проекты по точным именам; без имён - весь реестр.
func (e *Env) Named(names []string) ([]*config.Project, error) {
	if len(names) == 0 {
		out := make([]*config.Project, 0, len(e.Cfg.Projects))
		for i := range e.Cfg.Projects {
			out = append(out, &e.Cfg.Projects[i])
		}
		return out, nil
	}
	var out []*config.Project
	for _, n := range names {
		p, err := e.Cfg.Pick(n)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
