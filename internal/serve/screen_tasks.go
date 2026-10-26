package serve

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/recipes"
	"github.com/TimurTurdyev/tim/internal/tui"
)

// TaskPalette: группа палитры над выбранным проектом - его задачи и свои заготовки проекта.
func TaskPalette(env *app.Env) func(row tui.ProjectRow) (string, []tui.CommandEntry) {
	return func(row tui.ProjectRow) (string, []tui.CommandEntry) {
		var out []tui.CommandEntry
		for _, t := range recipes.Tasks(row.Dir) {
			out = append(out, tui.CommandEntry{Name: t.Name, Desc: strings.TrimSpace(t.Tool + " · " + t.Desc),
				Line: "run " + t.Name + " --in " + row.Name})
		}
		vars := map[string]string{"project": row.Name, "path": row.Dir}
		if p, ok := env.Cfg.Project(row.Name); ok {
			vars = projectVars(p, row.Dir)
		}
		for _, r := range recipes.Own(env.Cfg, recipes.OnProject) {
			out = append(out, ownEntry(r, recipes.Expand(r.Run, vars), "run "+r.Name+" --in "+row.Name))
		}
		return "задачи " + row.Name + " · этот проект · закроет tim", out
	}
}

// MachinePalette: свои заготовки машины - группа палитры на экране Машины.
func MachinePalette(env *app.Env) func() (string, []tui.CommandEntry) {
	return func() (string, []tui.CommandEntry) {
		wd, _ := os.Getwd()
		var out []tui.CommandEntry
		for _, r := range recipes.Own(env.Cfg, recipes.OnMachine) {
			out = append(out, ownEntry(r, recipes.Expand(r.Run, map[string]string{"path": wd}), "run "+r.Name))
		}
		if len(out) == 0 {
			return "", nil
		}
		return "свои заготовки · эта машина · закроет tim", out
	}
}

// ownEntry: своя заготовка строкой палитры; до первого согласия enter показывает окно с командой.
func ownEntry(r config.Recipe, script, line string) tui.CommandEntry {
	e := tui.CommandEntry{Name: r.Name, Desc: "своя · " + firstOf(r.Desc, r.Run), Line: line}
	if !recipes.Approved(script) {
		e.Confirm = &tui.Dialog{Title: "Своя заготовка " + r.Name,
			Lines:   []string{"tim не знает, что она меняет: спрошу один раз, пока команда не изменится."},
			Command: script, OK: "Выполнить",
			Yes: func() tea.Cmd { _ = recipes.Approve(script); return nil }}
	}
	return e
}
