package servers

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/recipes"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
)

// recipePalette: группа палитры на экранах сервера - заготовки, юниты из последнего отчёта, свои.
func recipePalette(env *app.Env, target string) (string, []tui.CommandEntry) {
	var out, units []tui.CommandEntry
	title := "на сервере " + target + " \u00b7 этот экран \u00b7 только чтение"
	line := func(r srvinfo.Recipe) string { return "server " + target + " --run " + r.ID }
	for _, r := range serverRecipes(env.Cfg, target) {
		e := tui.CommandEntry{Name: r.Name, Desc: r.Desc, Line: line(r)}
		if r.Param != "" {
			// Юниты - настоящие: упавшие и знакомые службы из последнего отчёта; свой - набрать.
			for _, u := range knownUnits(target) {
				units = append(units, tui.CommandEntry{Name: r.Name + " " + u.name, Desc: u.note, Line: line(r) + " " + u.name})
			}
			e.Name, e.Line, e.Template = r.Name+" ...", line(r)+" ", true
		}
		if r.Own {
			title = "на сервере " + target + " \u00b7 этот экран"
			e.Desc = "своя \u00b7 " + firstNonEmpty(r.Desc, "команда из конфига")
			if script, err := r.Script(""); err == nil && !recipes.Approved(script) {
				e.Confirm = &tui.Dialog{Title: "Своя заготовка " + r.ID + " на " + target,
					Lines:   []string{"tim не знает, только ли она читает: спрошу один раз, пока команда не изменится."},
					Command: strings.TrimSpace(script), OK: "Выполнить",
					Yes: func() tea.Cmd { _ = recipes.Approve(script); return nil }}
			}
		}
		out = append(out, e)
	}
	// Юниты - в конце: при пустом вводе их не видно, по "юнит" или имени находятся.
	return title, append(out, units...)
}

type unit struct{ name, note string }

// knownUnits: юниты для подстановки - упавшие первыми, потом службы, которые отчёт видел.
func knownUnits(target string) []unit {
	s, _, ok := saved(target)
	if !ok {
		return nil
	}
	var out []unit
	for _, u := range s.Report.Failed {
		out = append(out, unit{u, "упал"})
	}
	r := s.Report
	add := func(on bool, name string) {
		if on {
			out = append(out, unit{name, ""})
		}
	}
	add(r.Nginx != "", "nginx")
	add(r.Fail2ban, "fail2ban")
	add(r.CrowdSec, "crowdsec")
	add(len(r.SSHD) > 0 || r.NoSudoSSHD, "ssh")
	return out
}
