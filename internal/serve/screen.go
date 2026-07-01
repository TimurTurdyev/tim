package serve

import (
	"os"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Screen: фоновые dev-серверы - браузер, стоп и старт, журнал.
func Screen(env *app.Env) tui.Screen {
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Проекты", "Dev-серверы"},
		Command: func(row *tui.BoardRow) string {
			if row != nil {
				return "tim serve logs " + row.ID
			}
			return "tim serve list"
		},
		Load:  func(bool) tui.BoardData { return serveData() },
		Every: 5 * time.Second,
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "браузер", Run: func(row *tui.BoardRow) (string, error) {
				st, ok := stateOf(row)
				if !ok {
					return "", ui.Errorf("сервера нет")
				}
				url := "http://localhost:" + st.Port
				return "открыто " + url, runner.OpenBrowser(url)
			}},
			{Key: "s", Label: "стоп/старт", Run: func(row *tui.BoardRow) (string, error) {
				st, ok := stateOf(row)
				if !ok {
					return "", ui.Errorf("сервера нет")
				}
				if st.Alive() {
					if err := runner.StopGroup(st.PID); err != nil {
						return "", err
					}
					st.Remove()
					return st.Name + " остановлен", nil
				}
				return startAgain(env, st)
			}},
			{Key: "p", Label: "перезапуск", Run: func(row *tui.BoardRow) (string, error) {
				st, ok := stateOf(row)
				if !ok {
					return "", ui.Errorf("сервера нет")
				}
				if st.Alive() {
					if err := runner.StopGroup(st.PID); err != nil {
						return "", err
					}
					st.Remove()
				}
				return startAgain(env, st)
			}},
			{Key: "l", Label: "журнал", Do: func(row *tui.BoardRow) tui.Action {
				if row == nil {
					return tui.Action{}
				}
				name := row.ID
				return tui.Action{Push: tui.NewResult("журнал "+name, func() (string, error) {
					data, err := os.ReadFile(run.LogPath(name))
					if err != nil {
						return "", ui.Errorf("журнала %s нет", name)
					}
					lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
					if len(lines) > 300 {
						lines = lines[len(lines)-300:]
					}
					return strings.Join(lines, "\n"), nil
				}, tui.ResultOpts{Copy: runner.ToClipboard})}
			}},
		},
	})
}

func stateOf(row *tui.BoardRow) (run.State, bool) {
	if row == nil {
		return run.State{}, false
	}
	return run.LoadState(row.ID)
}

// startAgain: тот же проект на том же порту, вывод команды - в тост, а не на экран.
func startAgain(env *app.Env, st run.State) (string, error) {
	p, err := env.Cfg.Pick(st.Name)
	if err != nil {
		return "", err
	}
	var serr error
	_ = ui.Capture(func() { serr = background(env, []*config.Project{p}, st.Port, options{}) })
	if serr != nil {
		return "", serr
	}
	return st.Name + " запущен", nil
}

func serveData() tui.BoardData {
	d := tui.BoardData{Empty: "фоновых серверов нет: tim serve <проект> -d"}
	states := run.ListStates()
	alive := 0
	for _, s := range states {
		row := tui.BoardRow{ID: s.Name, Title: "журнал " + s.Name, Sub: "хвост, l - целиком"}
		age := ui.Duration(time.Since(s.Started))
		if s.Alive() {
			alive++
			row.Cells = ui.OK("\u25cf ") + ui.Pad(s.Name, 18) + ui.Pad(":"+s.Port, 8) + ui.Dim(ui.Pad(s.Command, 24)+ui.Pad(age, 9)) + ui.Info("http://localhost:"+s.Port)
		} else {
			row.Cells = ui.Bad("\u2715 ") + ui.Pad(s.Name, 18) + ui.Pad(":"+s.Port, 8) + ui.Dim(ui.Pad(s.Command, 24)) + ui.Bad("умер") + ui.Dim("  s - запустить снова")
		}
		row.Pane = logTail(run.LogPath(s.Name), 6)
		d.Rows = append(d.Rows, row)
	}
	d.Status = ui.Count(alive, "работает", "работают", "работают")
	if len(states) == 0 {
		d.Status = "нет фоновых"
	}
	return d
}

// logTail: последние строки журнала, ошибки красным.
func logTail(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{ui.Dim("журнала пока нет")}
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = ui.Clip(strings.TrimRight(l, "\r"), 110)
		low := strings.ToLower(l)
		switch {
		case strings.Contains(low, "error") || strings.Contains(low, "exception") || strings.Contains(low, " 500 ") || strings.Contains(low, "fatal"):
			out = append(out, ui.Bad(l))
		case strings.Contains(low, "warn"):
			out = append(out, ui.Warn(l))
		default:
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		return []string{ui.Dim("журнал пуст")}
	}
	return out
}
