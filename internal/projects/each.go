package projects

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/each"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// target: папка с маркером задачи, имя проекта и версия из маркера.
type target struct {
	dir, name, version string
}

// runEach: задача из конфига или разовая команда с --marker во всех папках с маркером.
func runEach(env *app.Env, args []string, dash int, o eachOptions) error {
	cfg := env.Cfg
	var t *config.Task
	var names []string
	switch {
	case o.marker != "":
		if dash < 0 || dash == len(args) {
			return ui.Errorf("команду пиши после --: tim each --marker go.mod -- go mod tidy")
		}
		t = &config.Task{Name: "разово", Marker: o.marker, Run: shellq.Join(args[dash:])}
		names = args[:dash]
	case len(args) == 0:
		printTasks(cfg)
		return nil
	default:
		found, ok := cfg.Task(args[0])
		if !ok {
			return ui.Errorf("нет задачи %s: tim each", args[0])
		}
		t, names = found, args[1:]
	}
	latest := latestVersion(t)
	targets := eachTargets(cfg, t, o.scan)
	if o.list {
		printTargets(t, targets, latest)
		return nil
	}
	todo := chooseTargets(targets, names, latest, o.all)
	if len(todo) == 0 {
		ui.Println(ui.Dim("делать нечего"))
		return nil
	}
	failed := 0
	for _, e := range todo {
		if !runTask(t, e, o.verbose) {
			failed++
		}
	}
	if failed > 0 {
		return ui.Errorf("не вышло: %d из %d", failed, len(todo))
	}
	return nil
}

func printTasks(cfg *config.Config) {
	if len(cfg.Tasks) == 0 {
		ui.Println(ui.Dim("задач нет: добавь [[task]] в конфиг (tim each --help) или tim each --marker файл -- команда"))
		return
	}
	for _, t := range cfg.Tasks {
		ui.Println(ui.Label(ui.Pad(t.Name, 14)) + " " + ui.Pad(t.Marker, 22) + " " + ui.Dim(t.Run))
	}
}

func printTargets(t *config.Task, targets []target, latest string) {
	old := 0
	for _, e := range targets {
		line := ui.Pad(e.name, 26)
		if t.VersionKey != "" {
			state := ui.Dim("свежая")
			if latest != "" && each.Older(e.version, latest) {
				state = ui.Warn("устарела")
				old++
			}
			line += " " + ui.Pad(ui.Dash(e.version), 8) + " " + state
		}
		ui.Println(line)
	}
	summary := t.Marker + ": " + strconv.Itoa(len(targets))
	if latest != "" {
		summary += ", устарело " + strconv.Itoa(old) + " (последняя " + latest + ")"
	}
	ui.Println(ui.Dim(summary))
}

// chooseTargets: названные проекты, а без имён - устаревшие (или все с all и без версии для сравнения).
func chooseTargets(targets []target, names []string, latest string, all bool) []target {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var todo []target
	for _, e := range targets {
		switch {
		case len(want) > 0:
			if want[e.name] {
				todo = append(todo, e)
			}
		case all || latest == "" || each.Older(e.version, latest):
			todo = append(todo, e)
		}
	}
	return todo
}

// runTask выполняет задачу в одной папке и печатает итог; false - не вышло.
func runTask(t *config.Task, e target, verbose bool) bool {
	out, err := runner.CaptureIn(e.dir, "sh", "-c", t.Run)
	if err != nil {
		ui.Println(ui.Bad(ui.Pad(e.name, 26)) + " " + err.Error())
		ui.Println(ui.Dim(ui.Tail(out, 5)))
		return false
	}
	if runner.DryRun {
		return true
	}
	line := ui.Label(ui.Pad(e.name, 26)) + " готово"
	if t.VersionKey != "" {
		line = ui.Label(ui.Pad(e.name, 26)) + " " + ui.Dash(e.version) + " -> " + each.JSONVersion(filepath.Join(e.dir, t.Marker), t.VersionKey)
	}
	ui.Println(line)
	if verbose && strings.TrimSpace(out) != "" {
		ui.Println(ui.Dim(strings.TrimRight(out, "\n")))
	}
	return true
}

// eachTargets: папки проектов из реестра (и над клоном, если задан wrapper) с маркером, а со scan -
// всё под корнем.
func eachTargets(cfg *config.Config, t *config.Task, scan bool) []target {
	names := map[string]string{}
	var candidates []string
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		dir := filepath.Clean(cfg.ProjectDir(p))
		candidates = append(candidates, dir)
		names[dir] = p.Name
		if cfg.Wrapper != "" && filepath.Base(dir) == cfg.Wrapper {
			candidates = append(candidates, filepath.Dir(dir))
			names[filepath.Dir(dir)] = p.Name
		}
	}
	if scan || t.Scan {
		candidates = append(candidates, each.Scan(cfg.Dev, t.Marker, 4)...)
	}
	var out []target
	for _, d := range each.Locate(candidates, t.Marker) {
		name := names[d]
		if name == "" {
			name = cfg.Relative(d)
		}
		e := target{dir: d, name: name}
		if t.VersionKey != "" {
			e.version = each.JSONVersion(filepath.Join(d, t.Marker), t.VersionKey)
		}
		out = append(out, e)
	}
	return out
}

// latestVersion: что печатает version_cmd; пусто - сравнивать не с чем.
func latestVersion(t *config.Task) string {
	if t.VersionCmd == "" || t.VersionKey == "" {
		return ""
	}
	out, err := runner.Output("sh", "-c", t.VersionCmd)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
