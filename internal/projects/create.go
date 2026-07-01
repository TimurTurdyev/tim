package projects

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/newproj"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/setup"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// newProject: папка по раскладке реестра, шаги шаблона, git init и запись в реестр.
func newProject(env *app.Env, name string, o newOptions) error {
	cfg := env.Cfg
	if strings.ContainsAny(name, "/ \t") || strings.HasPrefix(name, ".") {
		return ui.Errorf("имя без пробелов, слешей и точки в начале: %s", name)
	}
	if _, ok := cfg.Project(name); ok {
		return ui.Errorf("проект %s уже есть в реестре", name)
	}
	steps, needPort, err := templateSteps(cfg, o.stack)
	if err != nil {
		return err
	}
	dir, parent := newproj.Layout(cfg.Dev, o.in, name, cfg.Wrapper)
	if _, err := os.Stat(dir); err == nil {
		return ui.Errorf("папка уже есть: %s", dir)
	}
	ui.Println(ui.Label(name) + "  " + ui.Dim(o.stack+" -> "+dir))
	if err := mkdirAll(parent); err != nil {
		return err
	}
	for _, step := range steps {
		if err := runner.ExecIn(parent, nil, "sh", "-c", newproj.Expand(step, dir, name, parent)); err != nil {
			return ui.Errorf("шаг не прошёл: %s: %v; созданное осталось в %s", step, err, parent)
		}
	}
	if !o.noGit && !gitx.IsRepo(dir) {
		if err := runner.ExecIn(dir, nil, "git", "init", "-q"); err != nil {
			return ui.Errorf("git init: %v", err)
		}
	}
	p := config.Project{Name: name, Path: cfg.Relative(dir)}
	if needPort {
		p.Port = newproj.NextPort(cfg.Ports(), netinfo.DefaultPort, netinfo.BusyPorts().Busy)
	}
	if err := cfg.AddProject(p); err != nil {
		return err
	}
	if err := env.Save(); err != nil {
		return err
	}
	line := ui.Label("готово ") + name + ui.Dim("  "+p.Path)
	if p.Port > 0 {
		line += ui.Dim(", порт " + strconv.Itoa(p.Port))
	}
	ui.Println(line)
	ui.Println(ui.Dim("дальше: tim go " + name + ", tim serve " + name))
	if o.open {
		return env.Edit(dir)
	}
	return nil
}

// templateSteps: свой шаблон из конфига, иначе встроенный по стеку; port - нужен ли порт в реестре.
func templateSteps(cfg *config.Config, stack string) ([]string, bool, error) {
	for _, t := range cfg.Templates {
		if t.Name == stack {
			return t.Steps, t.Port, nil
		}
	}
	steps, ok := newproj.Builtin(stack, runner.Has("uv"))
	if !ok {
		var own []string
		for _, t := range cfg.Templates {
			own = append(own, t.Name)
		}
		return nil, false, ui.Errorf("нет шаблона %s: встроенные %s; свои [[template]] в конфиге: %s",
			stack, strings.Join(newproj.Stacks(), " "), ui.Dash(strings.Join(own, " ")))
	}
	return steps, stack == "laravel" || stack == "php", nil
}

// setupProject: от клона до работающего локально; адрес репозитория сначала клонируется.
func setupProject(env *app.Env, arg string, o setupOptions) error {
	var p *config.Project
	var dir string
	var err error
	fresh := gitURL(arg)
	if fresh {
		if p, dir, err = clone(env.Cfg, arg, o); err != nil {
			return err
		}
	} else {
		if p, err = env.Project(arg); err != nil {
			return err
		}
		dir = env.Cfg.ProjectDir(p)
		ui.Println(ui.Label(p.Name) + ui.Dim("  "+dir))
	}
	steps := setup.Plan(setup.Probe(dir))
	if len(steps) == 0 {
		ui.Println(ui.Dim("всё на месте, делать нечего"))
		return finishSetup(env, p, dir, fresh)
	}
	width := 0
	for _, s := range steps {
		width = max(width, ui.Width(s.Why))
	}
	for _, s := range steps {
		ui.Println("  " + ui.Label(ui.Pad(s.Why, width)) + "  " + ui.Dim(s.String()))
	}
	if !o.yes && !runner.Confirm("выполнить?") {
		return ui.Errorf("отменено")
	}
	for _, s := range steps {
		if s.Ask && !o.yes && !runner.Confirm(s.Why+": "+s.String()+"?") {
			continue
		}
		if err := doStep(dir, s); err != nil {
			return err
		}
	}
	return finishSetup(env, p, dir, fresh)
}

func doStep(dir string, s setup.Step) error {
	switch {
	case s.Touch != "":
		path := filepath.Join(dir, s.Touch)
		if err := mkdirAll(filepath.Dir(path)); err != nil {
			return err
		}
		return runner.WriteFile(path, nil, 0o600)
	case s.Cmd == nil:
		data, err := os.ReadFile(filepath.Join(dir, s.Copy[0]))
		if err != nil && !runner.DryRun {
			return err
		}
		return runner.WriteFile(filepath.Join(dir, s.Copy[1]), data, 0o600)
	}
	if err := runner.ExecIn(dir, nil, s.Cmd[0], s.Cmd[1:]...); err != nil {
		return ui.Errorf("%s: %s: %v", s.Why, s.String(), err)
	}
	return nil
}

// gitURL: аргумент похож на адрес репозитория, а не на имя проекта.
func gitURL(s string) bool {
	return strings.HasSuffix(s, ".git") || strings.HasPrefix(s, "git@") ||
		strings.HasPrefix(s, "https://") && strings.Count(s, "/") >= 4
}

// repoName: имя проекта из адреса репозитория.
func repoName(url string) string {
	name := strings.TrimSuffix(url, ".git")
	if i := strings.LastIndexAny(name, "/:"); i >= 0 {
		name = name[i+1:]
	}
	return name
}

// clone: репозиторий по раскладке реестра; проект попадёт в реестр после настройки.
func clone(cfg *config.Config, url string, o setupOptions) (*config.Project, string, error) {
	name := o.name
	if name == "" {
		name = repoName(url)
	}
	if name == "" {
		return nil, "", ui.Errorf("не понял имя проекта из %s: задай --name", url)
	}
	if _, ok := cfg.Project(name); ok {
		return nil, "", ui.Errorf("проект %s уже есть в реестре: tim setup %s", name, name)
	}
	dir, parent := newproj.Layout(cfg.Dev, o.in, name, cfg.Wrapper)
	if _, err := os.Stat(dir); err == nil {
		return nil, "", ui.Errorf("папка уже есть: %s", dir)
	}
	ui.Println(ui.Label(name) + ui.Dim("  "+url+" -> "+dir))
	if err := mkdirAll(parent); err != nil {
		return nil, "", err
	}
	if err := runner.ExecIn(parent, nil, "git", "clone", url, dir); err != nil {
		return nil, "", ui.Errorf("git clone: %v", err)
	}
	return &config.Project{Name: name, Path: cfg.Relative(dir), Remote: url}, dir, nil
}

// finishSetup: новый проект попадает в реестр уже с портом, подсмотренным у него самого.
func finishSetup(env *app.Env, p *config.Project, dir string, fresh bool) error {
	if fresh {
		// Порт выдаём только тем, кого потом можно поднять: репозиторию без стека он ни к чему.
		if p.Port == 0 && run.Servable(dir) {
			p.Port = newproj.NextPort(env.Cfg.Ports(), netinfo.DefaultPort, netinfo.BusyPorts().Busy)
		}
		if err := env.Cfg.AddProject(*p); err != nil {
			return err
		}
		if err := env.Save(); err != nil {
			return err
		}
	}
	line := ui.Label("готово ") + p.Name
	if p.Port > 0 {
		line += ui.Dim("  порт " + strconv.Itoa(p.Port))
	}
	ui.Println(line)
	ui.Println(ui.Dim("дальше: tim serve " + p.Name + ", tim go " + p.Name))
	return nil
}

// mkdirAll: папка под новый проект; в dry-run только строка команды.
func mkdirAll(dir string) error {
	if runner.DryRun {
		ui.Println(ui.Dim("$ mkdir -p " + dir))
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}
