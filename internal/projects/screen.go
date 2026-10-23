package projects

import (
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/visits"
)

// knownStack: стек проекта; неопознанный - пусто, а не "other": в списке это шум.
func knownStack(dir string) string {
	if s := run.Stack(dir); s != "other" {
		return s
	}
	return ""
}

// Screen: проекты недавние сверху; focus - кого показать первым.
func Screen(env *app.Env, focus string) tui.Screen {
	cfg := env.Cfg
	names := make([]string, len(cfg.Projects))
	for i, p := range cfg.Projects {
		names[i] = p.Name
	}
	seen, now := visits.Load(), time.Now()
	order := visits.Order(names, seen, now)
	rows := make([]tui.ProjectRow, 0, len(order))
	for _, i := range order {
		p := cfg.Projects[i]
		dir := cfg.ProjectDir(&p)
		row := tui.ProjectRow{Name: p.Name, Dir: dir, Stack: knownStack(dir)}
		if st, ok := run.LoadState(p.Name); ok && st.Alive() {
			row.Running, row.Port = true, st.Port
		}
		// Давность - с последнего захода: по заходам и сортировка "недавние сверху".
		if e, ok := seen[p.Name]; ok {
			row.Age = ui.Short(now.Sub(e.Last))
		}
		rows = append(rows, row)
	}
	if focus != "" {
		for i, r := range rows {
			if r.Name == focus && i > 0 {
				rows = append([]tui.ProjectRow{r}, append(rows[:i:i], rows[i+1:]...)...)
				break
			}
		}
	}
	return tui.NewProjects(tui.ProjectsOpts{
		Rows: rows,
		Card: func(row tui.ProjectRow) tui.ProjectCard { return projectCard(cfg, row) },
		Go:   func(p tui.ProjectRow) tui.Action { visits.Record(p.Name, time.Now()); return tui.Action{Dir: p.Dir} },
		Actions: []tui.ProjectAction{
			{Key: "o", Label: "редактор", Do: func(p tui.ProjectRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"open", p.Name}) }}
			}},
			{Key: "w", Label: "браузер", Do: func(p tui.ProjectRow) tui.Action { return env.Result([]string{"serve", "open", p.Name}) }},
			{Key: "d", Label: "dev-сервер", Do: func(p tui.ProjectRow) tui.Action {
				if p.Running {
					return env.Result([]string{"serve", "stop", p.Name})
				}
				return tui.Action{Exit: func() { env.After([]string{"serve", p.Name, "-d"}) }}
			}},
			{Key: "l", Label: "журнал", Do: func(p tui.ProjectRow) tui.Action { return env.Result([]string{"logs", p.Name}) }},
			{Key: "b", Label: "база", Do: func(p tui.ProjectRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"db", p.Name}) }}
			}},
			{Key: "c", Label: "проверить", Do: func(p tui.ProjectRow) tui.Action { return env.Result([]string{"check", p.Name}) }},
			{Key: "e", Label: ".env", Do: func(p tui.ProjectRow) tui.Action { return env.Result([]string{"env", p.Name}) }},
		},
	})
}

// projectCard: карточка проекта - считается при выборе, git спрашивается здесь, а не для всех сразу.
func projectCard(cfg *config.Config, row tui.ProjectRow) tui.ProjectCard {
	p, ok := cfg.Project(row.Name)
	if !ok {
		return tui.ProjectCard{}
	}
	label := func(s string) string { return ui.Dim(ui.Pad(s, 11)) }
	lines := []string{ui.Dim(paths.Show(row.Dir)), ""}
	stack := row.Stack
	if stack == "laravel" || stack == "php" {
		if v, err := runner.Output("php", "-r", "echo PHP_VERSION;"); err == nil {
			stack += ", php " + strings.TrimSpace(v)
		}
	}
	if stack != "" {
		lines = append(lines, label("стек")+stack)
	}
	if g, ok := gitx.Status(row.Dir); ok {
		lines = append(lines, label("ветка")+g.Line())
	}
	if row.Running {
		if st, ok := run.LoadState(row.Name); ok {
			lines = append(lines, label("dev-сервер")+ui.OK("\u25cf работает :"+st.Port)+ui.Dim(", "+ui.Duration(time.Since(st.Started))))
		}
	} else {
		lines = append(lines, label("dev-сервер")+ui.Dim("не запущен, порт "+run.ProjectPort(p, row.Dir)))
	}
	if p.URL != "" {
		lines = append(lines, label("сайт")+p.URL)
	}
	if p.DB != "" {
		lines = append(lines, label("база")+p.DB)
	}
	if p.SSH != "" {
		lines = append(lines, label("сервер")+p.SSH+ui.Dim("  "+p.Remote))
	}
	if p.Deploy != "" {
		lines = append(lines, label("деплой")+p.Deploy)
	}
	return tui.ProjectCard{Lines: lines}
}
