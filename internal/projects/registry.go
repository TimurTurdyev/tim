package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/rcfile"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/scan"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/visits"
)

// printProjects: реестр таблицей; group - начало имени или пути.
func printProjects(cfg *config.Config, group string) {
	ui.Println(ui.Dim(ui.Pad("ИМЯ", 24) + " " + ui.Pad("ПУТЬ", 42) + " " + ui.Pad("ПОРТ", 6) + " " + ui.Pad("БАЗА", 22) + " ДЕПЛОЙ"))
	for _, p := range cfg.Projects {
		if group != "" && !strings.HasPrefix(p.Path, group+"/") && !strings.HasPrefix(p.Name, group) {
			continue
		}
		ui.Println(ui.Label(ui.Pad(p.Name, 24)) + " " + ui.Pad(p.Path, 42) + " " + ui.Pad(portText(p.Port), 6) + " " +
			ui.Pad(ui.Dash(p.DB), 22) + " " + ui.Dim(ui.Dash(p.Deploy)))
	}
}

func printInfo(env *app.Env, name string) error {
	p, err := env.Project(name)
	if err != nil {
		return err
	}
	dir := env.Cfg.ProjectDir(p)
	ui.Println(ui.Label(p.Name) + "  " + ui.Dim(run.Stack(dir)))
	ui.Println(ui.Label("путь     ") + dir)
	ui.Println(ui.Label("ssh      ") + ui.Dash(p.SSH))
	if p.Remote != "" {
		ui.Println(ui.Label("удалённо ") + p.Remote)
	}
	ui.Println(ui.Label("деплой   ") + ui.Dash(p.Deploy))
	ui.Println(ui.Label("порт     ") + portText(p.Port) + "   " + ui.Label("база ") + ui.Dash(p.DB))
	if g, ok := gitx.Status(dir); ok {
		line := ui.Label("git      ") + g.Branch
		if g.Changes > 0 {
			line += "  " + ui.Warn(fmt.Sprintf("%d changes", g.Changes))
		}
		ui.Println(line)
	}
	return nil
}

func portText(port int) string {
	if port > 0 {
		return strconv.Itoa(port)
	}
	return "-"
}

func printHere(cfg *config.Config) error {
	cwd, _ := os.Getwd()
	p, ok := cfg.ProjectHere(cwd)
	if !ok {
		return ui.Errorf("не в проекте из конфига")
	}
	ui.Println(p.Name)
	return nil
}

// goTo: путь проекта для шима (--print) или с подсказкой, почему cd не сработал; без имени - выбор.
func goTo(env *app.Env, args []string, printOnly bool) error {
	cfg := env.Cfg
	if len(cfg.Projects) == 0 {
		return ui.Errorf("реестр пуст: tim config init найдёт клоны и предложит добавить")
	}
	if len(args) == 0 && !app.Interactive() {
		// Шим делает cd в то, что напечатано в stdout: список туда попасть не должен.
		if printOnly {
			ui.UseStderr()
			printProjects(cfg, "")
			return ui.Errorf("выбрать без терминала нельзя: tim go <имя>")
		}
		printProjects(cfg, "")
		return nil
	}
	p, err := goTarget(env, args)
	if err != nil {
		return err
	}
	dir := cfg.ProjectDir(p)
	if len(args) > 1 && args[1] == "." && cfg.Wrapper != "" && filepath.Base(dir) == cfg.Wrapper {
		dir = filepath.Dir(dir)
	}
	if _, err := os.Stat(dir); err != nil {
		return ui.Errorf("папки нет: %s", dir)
	}
	visits.Record(p.Name, time.Now())
	if printOnly {
		fmt.Println(dir)
		return nil
	}
	ui.Println(dir)
	ui.Println(ui.Dim(shimHint()))
	return nil
}

func goTarget(env *app.Env, args []string) (*config.Project, error) {
	if len(args) == 0 {
		return env.PickProject()
	}
	p, ok := env.Cfg.Project(args[0])
	if !ok {
		return nil, ui.Errorf("нет проекта %s: tim proj list", args[0])
	}
	return p, nil
}

// shimHint: почему cd не сработал - шим не подключён или оболочка открыта до подключения.
func shimHint() string {
	path, _ := rcfile.Path("")
	if rcfile.Sourced(path, 2) {
		return "эта оболочка открыта раньше подключения tim: открой новый терминал, и cd заработает"
	}
	return "чтобы cd работал из команды, выполни: tim init"
}

// scanRegistry: клоны под корнем, которых нет в реестре; add - добавить все.
func scanRegistry(env *app.Env, add bool) error {
	cfg := env.Cfg
	found, err := newClones(cfg)
	if err != nil {
		return err
	}
	if len(found) == 0 {
		ui.Println(ui.Dim("новых клонов в " + cfg.Dev + " нет"))
		return nil
	}
	for _, p := range found {
		ui.Println(ui.Label(ui.Pad(p.Name, 24)) + " " + p.Path)
	}
	if !add && !app.Interactive() {
		ui.Println(ui.Dim("добавить: tim proj scan --add"))
		return nil
	}
	chosen, err := chooseProjects(found, add)
	if err != nil || len(chosen) == 0 {
		return err
	}
	if add && !runner.Confirm("добавить в реестр "+strconv.Itoa(len(chosen))+"?") {
		return nil
	}
	n := addAll(cfg, chosen)
	if err := env.Save(); err != nil {
		return err
	}
	ui.Println(ui.Label("добавлено ") + strconv.Itoa(n))
	return nil
}

// addProject: папка в реестр; имя - заданное или подобранное по пути.
func addProject(env *app.Env, dir, name, url string) error {
	cfg := env.Cfg
	if dir == "" {
		dir, _ = os.Getwd()
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		return ui.Errorf("папки нет: %s", abs)
	}
	for i := range cfg.Projects {
		if filepath.Clean(cfg.ProjectDir(&cfg.Projects[i])) == abs {
			return ui.Errorf("%s уже в реестре как %s", abs, cfg.Projects[i].Name)
		}
	}
	if name == "" {
		name = scan.Names([]string{abs}, cfg.Wrapper, takenNames(cfg))[0]
	}
	if err := cfg.AddProject(config.Project{Name: name, Path: cfg.Relative(abs), URL: url}); err != nil {
		return err
	}
	if err := env.Save(); err != nil {
		return err
	}
	ui.Println(ui.Label("добавлен ") + name + "  " + ui.Dim(cfg.Relative(abs)))
	return nil
}

func removeProject(env *app.Env, name string) error {
	if _, ok := env.Cfg.Project(name); !ok {
		return ui.Errorf("нет проекта %s в реестре", name)
	}
	if !runner.Confirm("убрать " + name + " из реестра?") {
		return nil
	}
	env.Cfg.RemoveProject(name)
	if err := env.Save(); err != nil {
		return err
	}
	ui.Println(ui.Label("убран ") + name)
	return nil
}

func takenNames(cfg *config.Config) map[string]bool {
	taken := map[string]bool{}
	for _, p := range cfg.Projects {
		taken[p.Name] = true
	}
	return taken
}
