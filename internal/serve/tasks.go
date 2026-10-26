package serve

import (
	"os"
	"strconv"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/recipes"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// runTask: своя заготовка машины, своя заготовка проекта или задача проекта - по имени; без имени - список.
func runTask(env *app.Env, name, in string) error {
	cfg := env.Cfg
	if r, ok := recipes.Find(cfg, recipes.OnMachine, name); ok && name != "" {
		wd, _ := os.Getwd()
		return runOwn(r, wd, recipes.Expand(r.Run, map[string]string{"path": wd}))
	}
	p, err := env.Project(in)
	if err != nil {
		if name == "" {
			printRecipes("свои заготовки машины", recipes.Own(cfg, recipes.OnMachine))
		}
		return err
	}
	dir := cfg.ProjectDir(p)
	if name == "" {
		listTasks(cfg, p)
		return nil
	}
	if r, ok := recipes.Find(cfg, recipes.OnProject, name); ok {
		return runOwn(r, dir, recipes.Expand(r.Run, projectVars(p, dir)))
	}
	for _, t := range recipes.Tasks(dir) {
		if t.Name == name {
			args := t.Args()
			if !runner.Has(args[0]) {
				return ui.Errorf("%s: задача %s запускается через %s, а его нет в PATH", p.Name, name, args[0])
			}
			return runner.ExecIn(dir, nil, args[0], args[1:]...)
		}
	}
	return ui.Errorf("%s: задачи %s нет; список - tim run --in %s", p.Name, name, p.Name)
}

func projectVars(p *config.Project, dir string) map[string]string {
	vars := map[string]string{"project": p.Name, "path": dir}
	if p.Port > 0 {
		vars["port"] = strconv.Itoa(p.Port)
	}
	return vars
}

// runOwn: своя заготовка - с согласия при первом запуске.
func runOwn(r config.Recipe, dir, script string) error {
	if !runner.DryRun && !recipes.Approved(script) {
		ui.Println(ui.Warn("своя заготовка "+r.Name+": tim не знает, что она меняет") + "\n  " + script)
		if !runner.Confirm("выполнить в " + paths.Show(dir) + "?") {
			return ui.Errorf("не выполнял: своя заготовка запускается с согласия, в скрипте - с -y")
		}
		_ = recipes.Approve(script)
	}
	return runner.ExecIn(dir, nil, "sh", "-c", script)
}

func listTasks(cfg *config.Config, p *config.Project) {
	dir := cfg.ProjectDir(p)
	ui.Println(ui.Label(p.Name) + "  " + ui.Dim(paths.Show(dir)))
	tasks := recipes.Tasks(dir)
	if len(tasks) == 0 {
		ui.Println("  " + ui.Dim("задач нет: ни Makefile, ни justfile, ни Taskfile, ни scripts в package.json и composer.json"))
	}
	for _, t := range tasks {
		ui.Println("  " + ui.Pad(t.Name, 22) + ui.Pad(t.Tool, 10) + ui.Dim(ui.Clip(t.Desc, 70)))
	}
	printRecipes("свои заготовки", append(recipes.Own(cfg, recipes.OnProject), recipes.Own(cfg, recipes.OnMachine)...))
}

func printRecipes(title string, list []config.Recipe) {
	if len(list) == 0 {
		return
	}
	ui.Println("\n" + ui.Label(title))
	for _, r := range list {
		ui.Println("  " + ui.Pad(r.Name, 22) + ui.Pad(r.On, 10) + ui.Dim(ui.Clip(firstOf(r.Desc, r.Run), 70)))
	}
}

func firstOf(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}
