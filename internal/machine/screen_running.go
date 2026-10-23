package machine

import (
	"os"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Проекты, вкладка Запущенные: проекты, которые слушают порты, по тому же скану, что у главного
// экрана и сводки. Поднятые через tim и вручную - с честными действиями для каждого.

// otherPorts: строка "N портов не у проектов" - ведёт в tim ports.
const otherPorts = "\x00ports"

// RunningPorts: проект -> его порт по скану портов.
func RunningPorts(cfg *config.Config) map[string]string {
	out := map[string]string{}
	for _, r := range Ports(cfg) {
		if r.Project != "" && !r.Guessed {
			if _, ok := out[r.Project]; !ok {
				out[r.Project] = r.Port
			}
		}
	}
	return out
}

// RunningTab: вкладка Запущенные.
func RunningTab(env *app.Env) tui.Tab {
	stop := func(row *tui.BoardRow) (string, error) { return stopServer(env, row.ID) }
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Проекты"},
		Tab:    "Запущенные",
		Search: true,
		Command: func(*tui.BoardRow) string {
			return "tim serve list"
		},
		Load: func(bool) tui.BoardData { return runningData(env.Cfg) },
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "перейти", Do: func(row *tui.BoardRow) tui.Action {
				if row.ID == otherPorts {
					return env.Result([]string{"ports"})
				}
				return tui.Action{Dir: row.Find}
			}},
			{Key: "w", Label: "браузер", Doing: "открываю в браузере", Run: func(row *tui.BoardRow) (string, error) {
				return row.ID + ": открыт в браузере", runner.OpenBrowser("http://localhost:" + portOf(env.Cfg, row.ID))
			}},
			{Key: "d", Label: "погасить", Doing: "гашу dev-сервер", Run: stop},
			{Key: "u", Label: "перезапуск", Doing: "перезапускаю dev-сервер", Run: func(row *tui.BoardRow) (string, error) {
				if _, err := stopServer(env, row.ID); err != nil {
					return "", err
				}
				if _, err := env.Inside([]string{"serve", row.ID, "-d"}); err != nil {
					return "", err
				}
				return row.ID + ": перезапущен через tim", nil
			}},
			{Key: "l", Label: "журнал", Do: func(row *tui.BoardRow) tui.Action { return env.Result([]string{"serve", "logs", row.ID}) }},
		},
	}).(tui.Tab)
}

func runningData(cfg *config.Config) tui.BoardData {
	var d tui.BoardData
	rows := Ports(cfg)
	others := 0
	for _, r := range rows {
		if r.Project == "" || r.Guessed {
			others++
			continue
		}
		d.Rows = append(d.Rows, runningRow(r))
	}
	seen := map[string]bool{}
	for _, r := range d.Rows {
		seen[r.ID] = true
	}
	n := len(seen)
	d.Status = ui.Count(n, "запущен", "запущено", "запущено")
	d.Tab = "Запущенные " + strconv.Itoa(n)
	d.Columns = ui.Fit("ПРОЕКТ", 27) + ui.Fit("ПОРТ", 8) + ui.Fit("КАК", 10) + "РАБОТАЕТ"
	d.Empty = "Запущенных проектов нет"
	if others > 0 && n > 0 {
		d.Rows = append(d.Rows, tui.BoardRow{ID: otherPorts, Cells: ui.Dim(ui.Count(others, "порт", "порта", "портов") + " не у проектов"),
			Inline: "enter - кто слушает: tim ports"})
	}
	return d
}

func runningRow(r PortRow) tui.BoardRow {
	st, viaTim := run.LoadState(r.Project)
	viaTim = viaTim && st.Alive() && st.Port == r.Port
	how := ui.Dim("вручную")
	if viaTim {
		how = "tim"
	}
	up := ui.Short(r.Uptime)
	row := tui.BoardRow{ID: r.Project, Find: r.Dir,
		Cells: ui.Fit(r.Project, 27) + ui.Fit(":"+r.Port, 8) + ui.Fit(how, 10) + ui.Dim(up)}
	c := &tui.Card{Title: r.Project, Sub: strings.Join(nonEmptyParts(r.Kind, up), " \u00b7 "), Path: paths.Show(r.Dir),
		Enter: "перейти в папку, tim закроется"}
	c.Facts = append(c.Facts, tui.Fact{Label: "адрес", Value: ui.Info("http://localhost:" + r.Port)})
	if viaTim {
		c.Facts = append(c.Facts, tui.Fact{Label: "команда", Value: st.Command}, tui.Fact{Label: "журнал", Value: ui.Dim(paths.Show(run.LogPath(r.Project)))})
		if tail := logTail(run.LogPath(r.Project), 4); len(tail) > 0 {
			c.Extra = append([]string{"", ui.Dim("ЖУРНАЛ \u00b7 последние " + strconv.Itoa(len(tail)))}, tail...)
		}
		c.Actions = []tui.CardAction{{Key: "w", Label: "браузер"}, {Key: "d", Label: "погасить"}, {Key: "u", Label: "перезапуск"}, {Key: "l", Label: "журнал"}}
	} else {
		c.Facts = append(c.Facts, tui.Fact{Label: "процесс", Value: "pid " + r.Proc.PID},
			tui.Fact{Label: "журнал", Value: ui.Dim("нет: запущен не через tim, перезапуск поднимет через tim")})
		c.Actions = []tui.CardAction{{Key: "w", Label: "браузер"}, {Key: "d", Label: "погасить"}, {Key: "u", Label: "перезапуск"}}
	}
	row.Card = c
	return row
}

// stopServer: свой сервер tim гасит группой процессов, чужой - как tim port N kill.
func stopServer(env *app.Env, name string) (string, error) {
	if st, ok := run.LoadState(name); ok && st.Alive() {
		if err := runner.StopGroup(st.PID); err != nil {
			return "", err
		}
		st.Remove()
		return name + ": остановлен", nil
	}
	port := portOf(env.Cfg, name)
	if port == "" {
		return "", ui.Errorf("%s уже не слушает порт", name)
	}
	if _, err := env.Inside([]string{"port", port, "kill", "-y"}); err != nil {
		return "", err
	}
	return "порт " + port + ": свободен", nil
}

func portOf(cfg *config.Config, name string) string { return RunningPorts(cfg)[name] }

// logTail: последние строки журнала как есть.
func logTail(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, "\r")
	}
	return lines
}

func nonEmptyParts(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
