package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/visits"
)

// ---------- проекты ----------

func projectsScreen(focus string) tui.Screen {
	names := make([]string, len(cfg.Projects))
	for i, p := range cfg.Projects {
		names[i] = p.Name
	}
	order := visits.Order(names, visits.Load(), time.Now())
	rows := make([]tui.ProjectRow, 0, len(order))
	for _, i := range order {
		p := cfg.Projects[i]
		dir := cfg.ProjectDir(&p)
		row := tui.ProjectRow{Name: p.Name, Dir: dir, Stack: run.Stack(dir)}
		if st, ok := run.LoadState(p.Name); ok && st.Alive() {
			row.Running, row.Port = true, st.Port
		}
		if info, err := os.Stat(dir); err == nil {
			row.Age = agoShort(info.ModTime())
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
		Card: projectCard,
		Go:   func(p tui.ProjectRow) tui.Action { visits.Record(p.Name, time.Now()); return tui.Action{Dir: p.Dir} },
		Actions: []tui.ProjectAction{
			{Key: "g", Label: "папка", Do: func(p tui.ProjectRow) tui.Action { visits.Record(p.Name, time.Now()); return tui.Action{Dir: p.Dir} }},
			{Key: "o", Label: "редактор", Do: func(p tui.ProjectRow) tui.Action {
				return tui.Action{Exit: func() { runAfter([]string{"open", p.Name}) }}
			}},
			{Key: "b", Label: "сайт", Do: func(p tui.ProjectRow) tui.Action { return resultAction([]string{"serve", "open", p.Name}) }},
			{Key: "s", Label: "сервер", Do: func(p tui.ProjectRow) tui.Action {
				if p.Running {
					return resultAction([]string{"serve", "stop", p.Name})
				}
				return tui.Action{Exit: func() { runAfter([]string{"serve", p.Name, "-d"}) }}
			}},
			{Key: "l", Label: "журнал", Do: func(p tui.ProjectRow) tui.Action { return resultAction([]string{"logs", p.Name}) }},
			{Key: "d", Label: "база", Do: func(p tui.ProjectRow) tui.Action {
				return tui.Action{Exit: func() { runAfter([]string{"db", p.Name}) }}
			}},
			{Key: "c", Label: "проверить", Do: func(p tui.ProjectRow) tui.Action { return resultAction([]string{"check", p.Name}) }},
			{Key: "e", Label: ".env", Do: func(p tui.ProjectRow) tui.Action { return resultAction([]string{"env", p.Name}) }},
		},
	})
}

// projectCard: карточка проекта - считается при выборе, git спрашивается здесь, а не для всех сразу.
func projectCard(row tui.ProjectRow) tui.ProjectCard {
	p, ok := cfg.Project(row.Name)
	if !ok {
		return tui.ProjectCard{}
	}
	label := func(s string) string { return ui.Dim(ui.Pad(s, 9)) }
	lines := []string{ui.Dim(tilde(rel(row.Dir))), ""}
	stack := row.Stack
	if v, err := runner.Output("php", "-r", "echo PHP_VERSION;"); err == nil && (stack == "laravel" || stack == "php") {
		stack += ", php " + strings.TrimSpace(v)
	}
	lines = append(lines, label("стек")+stack)
	if out, err := runner.Output("git", "-C", row.Dir, "status", "-sb"); err == nil {
		g := sysinfo.ParseGitStatusSB(out)
		lines = append(lines, label("ветка")+gitLine(g))
	}
	if row.Running {
		if st, ok := run.LoadState(row.Name); ok {
			lines = append(lines, label("сервер")+ui.OK("\u25cf работает :"+st.Port)+ui.Dim(", "+ui.Duration(time.Since(st.Started))))
		}
	} else {
		lines = append(lines, label("сервер")+ui.Dim("\u25cb не запущен, порт "+run.ProjectPort(p, row.Dir)))
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

// agoShort: давность одним числом - для колонок списка.
func agoShort(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d ч", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%d дн", int(d.Hours()/24))
	}
	return fmt.Sprintf("%d мес", int(d.Hours()/24/30))
}

// rel: путь от домашней папки для показа.
func rel(p string) string {
	home, _ := os.UserHomeDir()
	if r, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(r, "..") {
		return r
	}
	return p
}
