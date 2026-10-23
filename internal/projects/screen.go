package projects

import (
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/visits"
)

// Вкладки экрана проектов.
const (
	TabAll = iota
	TabRunning
	TabCheck
)

// knownStack: стек проекта; неопознанный - пусто, а не "other": в списке это шум.
func knownStack(dir string) string {
	if s := run.Stack(dir); s != "other" {
		return s
	}
	return ""
}

// AllTab: вкладка "Все" - недавние сверху; focus - кого показать первым; running - запущенные по
// скану портов, тот же источник, что у главного экрана.
func AllTab(env *app.Env, focus string, running func() map[string]string) tui.Tab {
	cfg := env.Cfg
	seen, now := visits.Load(), time.Now()
	rows := make([]tui.ProjectRow, 0, len(cfg.Projects))
	for i := range cfg.Projects {
		p := &cfg.Projects[i]
		dir := cfg.ProjectDir(p)
		row := tui.ProjectRow{Name: p.Name, Dir: dir, Folder: folder(cfg, dir)}
		// Давность - с последнего захода: по заходам и сортировка "недавние сверху".
		if e, ok := seen[p.Name]; ok {
			row.Age, row.Visited = ui.Short(now.Sub(e.Last)), true
		}
		rows = append(rows, row)
	}
	order := visits.Order(names(cfg), seen, now)
	sorted := make([]tui.ProjectRow, 0, len(rows))
	for _, i := range order {
		sorted = append(sorted, rows[i])
	}
	if focus != "" {
		for i, r := range sorted {
			if r.Name == focus && i > 0 {
				sorted = append([]tui.ProjectRow{r}, append(sorted[:i:i], sorted[i+1:]...)...)
				break
			}
		}
	}
	return tui.NewProjects(tui.ProjectsOpts{
		Rows:    sorted,
		Running: running,
		Card:    func(row tui.ProjectRow) tui.Card { return projectCard(cfg, row) },
		Go:      func(p tui.ProjectRow) tui.Action { visits.Record(p.Name, time.Now()); return tui.Action{Dir: p.Dir} },
		Actions: actions(env),
		Empty:   []tui.Key{{K: "enter", D: "найти"}, {K: "n", D: "добавить"}},
		OnEmpty: func(key string) tui.Action {
			switch key {
			case "enter":
				return env.Result([]string{"proj", "scan", "--add"})
			case "n":
				return env.Result([]string{"proj", "add", "."})
			}
			return tui.Action{}
		},
	})
}

func names(cfg *config.Config) []string {
	out := make([]string, len(cfg.Projects))
	for i, p := range cfg.Projects {
		out[i] = p.Name
	}
	return out
}

// folder: папка проекта относительно корня проектов, для колонки ПАПКА на широком окне.
func folder(cfg *config.Config, dir string) string {
	root := config.ExpandHome(cfg.Dev)
	if rel, ok := strings.CutPrefix(dir, root+"/"); ok {
		return rel
	}
	return paths.Show(dir)
}

// actions: действия над проектом. dev-сервер, его стоп и редактор с окном - в фоне, tim остаётся;
// закрывают tim только cd, клиент базы, ssh и редактор в терминале - это написано у капа.
func actions(env *app.Env) []tui.ProjectAction {
	cfg := env.Cfg
	edit := tui.ProjectAction{Key: "o", Label: "редактор",
		Doing: func(p tui.ProjectRow) string { return "открываю " + p.Name + " в редакторе" },
		Run: func(p tui.ProjectRow) (string, error) {
			return p.Name + ": открыт в редакторе", env.Edit(p.Dir)
		}}
	if env.EditorInTerminal() {
		edit = tui.ProjectAction{Key: "o", Label: "редактор", Note: "закроет tim", Do: func(p tui.ProjectRow) tui.Action {
			return tui.Action{Exit: func() { env.After([]string{"open", p.Name}) }}
		}}
	}
	has := func(field func(*config.Project) string) func(tui.ProjectRow) bool {
		return func(r tui.ProjectRow) bool {
			p, ok := cfg.Project(r.Name)
			return ok && field(p) != ""
		}
	}
	return []tui.ProjectAction{
		edit,
		{Key: "w", Label: "браузер", Doing: func(p tui.ProjectRow) string { return "открываю " + p.Name + " в браузере" },
			Run: func(p tui.ProjectRow) (string, error) { return env.Inside([]string{"serve", "open", p.Name}) }},
		{Key: "d", Label: "dev-сервер",
			LabelFor: func(p tui.ProjectRow) string {
				if p.Port != "" {
					return "погасить"
				}
				return "dev-сервер"
			},
			Doing: func(p tui.ProjectRow) string {
				if p.Port != "" {
					return "гашу " + p.Name + " на :" + p.Port
				}
				return "поднимаю dev-сервер " + p.Name
			},
			Run: func(p tui.ProjectRow) (string, error) { return devServer(env, p) }},
		{Key: "l", Label: "журнал", Do: func(p tui.ProjectRow) tui.Action { return env.Result([]string{"logs", p.Name}) }},
		{Key: "b", Label: "база", Note: "закроет tim", When: has(func(p *config.Project) string { return p.DB }),
			Do: func(p tui.ProjectRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"db", p.Name}) }}
			}},
		{Key: "s", Label: "ssh", Note: "закроет tim", When: has(func(p *config.Project) string { return p.SSH }),
			Do: func(p tui.ProjectRow) tui.Action {
				return tui.Action{Exit: func() { env.After([]string{"ssh", p.Name}) }}
			}},
		{Key: "c", Label: "проверить", Do: func(p tui.ProjectRow) tui.Action { return env.Result([]string{"check", p.Name}) }},
		{Key: "e", Label: ".env", Do: func(p tui.ProjectRow) tui.Action { return env.Result([]string{"env", p.Name}) }},
	}
}

// devServer: поднять в фоне или погасить то, что слушает его порт - через tim или вручную.
func devServer(env *app.Env, p tui.ProjectRow) (string, error) {
	if p.Port == "" {
		if _, err := env.Inside([]string{"serve", p.Name, "-d"}); err != nil {
			return "", err
		}
		if st, ok := run.LoadState(p.Name); ok {
			return p.Name + ": запущен на :" + st.Port, nil
		}
		return p.Name + ": запущен", nil
	}
	if st, ok := run.LoadState(p.Name); ok && st.Alive() && st.Port == p.Port {
		if err := runner.StopGroup(st.PID); err != nil {
			return "", err
		}
		st.Remove()
		return p.Name + ": остановлен", nil
	}
	if _, err := env.Inside([]string{"port", p.Port, "kill", "-y"}); err != nil {
		return "", err
	}
	return "порт " + p.Port + ": свободен", nil
}

// projectCard: карточка проекта - считается при выборе, git спрашивается здесь, а не для всех сразу.
func projectCard(cfg *config.Config, row tui.ProjectRow) tui.Card {
	c := tui.Card{Title: row.Name, Path: paths.Show(row.Dir)}
	p, ok := cfg.Project(row.Name)
	if !ok {
		return c
	}
	stack := knownStack(row.Dir)
	c.Sub = strings.Join(nonEmpty(stack, row.Age), " \u00b7 ")
	if g, ok := gitx.Status(row.Dir); ok {
		c.Facts = append(c.Facts, tui.Fact{Label: "ветка", Value: g.Line()})
	}
	c.Facts = append(c.Facts, tui.Fact{Label: "dev-сервер", Value: devLine(p, row)})
	saved, haveSaved := health.SavedRow(row.Name)
	if p.URL != "" {
		v := ui.Info(p.URL)
		if haveSaved {
			if s := saved.Get("сайт"); s.Level != level.Skip {
				v += ui.Dim(" \u00b7 ") + level.Lamp(s.Level, s.Text)
			}
		}
		c.Facts = append(c.Facts, tui.Fact{Label: "сайт", Value: v})
	}
	if haveSaved {
		if s := saved.Get("сертификат"); s.Level != level.Skip {
			c.Facts = append(c.Facts, tui.Fact{Label: "сертификат", Value: level.Lamp(s.Level, s.Text) + ui.Dim(" \u00b7 проверено ") + age(saved.At)})
		}
	}
	if p.DB != "" {
		c.Facts = append(c.Facts, tui.Fact{Label: "база", Value: p.DB})
	}
	if p.SSH != "" {
		c.Facts = append(c.Facts, tui.Fact{Label: "сервер", Value: p.SSH})
		if p.Remote != "" {
			c.Facts = append(c.Facts, tui.Fact{Value: ui.Dim(p.Remote)})
		}
	}
	if p.Deploy != "" {
		c.Facts = append(c.Facts, tui.Fact{Label: "деплой", Value: p.Deploy})
	}
	return c
}

// devLine: запущен ли dev-сервер, через tim ли и сколько работает; не запущен - на каком порту встанет.
func devLine(p *config.Project, row tui.ProjectRow) string {
	if row.Port == "" {
		return ui.Dim("не запущен \u00b7 порт " + run.ProjectPort(p, row.Dir))
	}
	line := ui.OK("\u25cf :" + row.Port)
	if st, ok := run.LoadState(row.Name); ok && st.Alive() && st.Port == row.Port {
		return line + ui.Dim(" \u00b7 работает "+ui.Short(time.Since(st.Started)))
	}
	return line + ui.Dim(" \u00b7 не через tim")
}

// age: давность проверки одной единицей.
func age(t time.Time) string {
	if time.Since(t) < time.Minute {
		return "только что"
	}
	if time.Since(t) > health.StaleAfter {
		return ui.Warn(ui.Short(time.Since(t)))
	}
	return ui.Dim(ui.Short(time.Since(t)))
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
