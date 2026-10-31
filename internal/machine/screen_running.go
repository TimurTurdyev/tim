package machine

import (
	"cmp"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Машина, вкладка Запущенные: всё, что слушает порт, кроме системного, - в какой папке, кто запустил
// (ты в терминале или агент и его сессия) и сколько работает. Забытое - группой сверху: сессия агента
// закрыта, агент молчит, а процесс всё работает, или запущенное вручную висит дольше 8 часов.

// runningTab: вкладка Запущенные; строки - по pid, действия берут строку из последнего скана.
func runningTab(env *app.Env) tui.Tab {
	byID := map[string]PortRow{}
	row := func(b *tui.BoardRow) (PortRow, bool) {
		r, ok := byID[b.ID]
		return r, ok
	}
	return tui.NewBoard(tui.BoardOpts{
		Crumbs: []string{"Машина"}, Tab: "Запущенные", Search: true,
		Loading: "смотрю, кто слушает порты и кто их запустил",
		Command: func(*tui.BoardRow) string { return "tim ports" },
		Load: func(bool) tui.BoardData {
			rows := PortsFull(env.Cfg)
			clear(byID)
			for _, r := range rows {
				byID[rowID(r)] = r
			}
			return runningData(rows)
		},
		Actions: []tui.BoardAction{
			{Key: "enter", Label: "перейти", When: func(b *tui.BoardRow) bool { r, _ := row(b); return r.Dir != "" },
				Do: func(b *tui.BoardRow) tui.Action { r, _ := row(b); return tui.Action{Dir: r.Dir} }},
			{Key: "w", Label: "браузер", Doing: "открываю в браузере", Run: func(b *tui.BoardRow) (string, error) {
				r, _ := row(b)
				return ":" + r.Port + " открыт в браузере", runner.OpenBrowser("http://localhost:" + r.Port)
			}},
			{Key: "x", Label: "закрыть", Doing: "закрываю", Run: func(b *tui.BoardRow) (string, error) {
				r, ok := row(b)
				if !ok {
					return "", nil
				}
				return closeRow(r, false)
			}},
			{Key: "X", Label: "принудительно", Doing: "закрываю принудительно", Hidden: true, Run: func(b *tui.BoardRow) (string, error) {
				r, ok := row(b)
				if !ok {
					return "", nil
				}
				return closeRow(r, true)
			}},
		},
	}).(tui.Tab)
}

func rowID(r PortRow) string {
	if r.Listener.PID != "" {
		return r.Listener.PID
	}
	return "port " + r.Port
}

// shown: что показывать во вкладке - то же, что tim ports без --all.
func shown(r PortRow) bool {
	port, _ := strconv.Atoi(r.Port)
	return r.Dir != "" || r.Agent != nil || !procs.System(r.Proc.Command) && port <= 49151
}

func runningData(rows []PortRow) tui.BoardData {
	var d tui.BoardData
	var forgotten, working, services []PortRow
	for _, r := range rows {
		switch {
		case !shown(r):
		case r.Forgotten(ForgottenAfter):
			forgotten = append(forgotten, r)
		case r.Dir != "" || r.Agent != nil:
			working = append(working, r)
		default:
			services = append(services, r)
		}
	}
	n := len(forgotten) + len(working) + len(services)
	d.Tab = "Запущенные " + strconv.Itoa(n)
	d.Status = ui.Count(n, "процесс", "процесса", "процессов")
	if len(forgotten) > 0 {
		d.Status += " · " + strconv.Itoa(len(forgotten)) + " забыто"
	}
	d.Columns = ui.Fit("ПОРТ", 15) + ui.Fit("ЧТО", 18) + ui.Fit("ПАПКА", 20) + ui.Fit("КТО", 40) + "РАБОТАЕТ"
	d.Wide = true
	add := func(list []PortRow, group string) {
		for _, r := range list {
			b := runningRow(r)
			b.Group = group + " · " + strconv.Itoa(len(list))
			d.Rows = append(d.Rows, b)
		}
	}
	add(forgotten, "ЗАБЫТЫЕ")
	add(working, "РАБОТАЮТ")
	add(services, "СЛУЖБЫ")
	d.Empty = "Ничего не запущено: ни dev-серверов, ни служб"
	return d
}

func runningRow(r PortRow) tui.BoardRow {
	ports := ":" + strings.Join(r.Ports, ", :")
	lead := ports
	if r.Forgotten(ForgottenAfter) {
		lead = ui.Warn("▲ ") + ports
	}
	who := whoShort(r)
	if who == "" && r.Dir != "" {
		who = ui.Dim("ты")
	}
	b := tui.BoardRow{ID: rowID(r), Find: r.Folder + " " + r.Kind + " " + r.Dir,
		Cells: ui.Fit(lead, 15) + ui.Fit(ui.Dash(r.Kind), 18) + ui.Fit(r.Folder, 20) + ui.Fit(who, 40) + ui.Dim(ui.Short(r.Uptime))}
	c := &tui.Card{Title: ports + "  " + ui.Dash(r.Kind), Sub: paths.Show(cmp.Or(r.Dir, r.Cwd))}
	if r.Dir != "" {
		c.Enter = "перейти в папку, tim закроется"
	}
	fact := func(label, value string) {
		if value != "" {
			c.Facts = append(c.Facts, tui.Fact{Label: label, Value: value})
		}
	}
	fact("запустил", starter(r))
	fact("работает", ui.Short(r.Uptime)+ui.Dim(" · память "+ui.Bytes(r.RSS)))
	fact("команда", ui.Clip(shortArgs(r), 110))
	if out, _ := procs.Outputs(r.Listener.PID); out != "" {
		fact("вывод", ui.Dim(paths.Show(out)))
	}
	if r.Network && devServer(r) {
		fact("в сети", ui.Warn("слушает все адреса: его видно всем в этой сети")+ui.Dim(" · только себе - --host 127.0.0.1"))
	}
	if r.Forgotten(ForgottenAfter) {
		fact("забыт", ui.Warn(forgottenWhy(r))+ui.Dim(" · x - закрыть"))
	}
	c.Actions = []tui.CardAction{{Key: "w", Label: "браузер"}, {Key: "x", Label: "закрыть"}}
	b.Card = c
	return b
}

// shortArgs: команда без длинного пути к программе - в карточке важнее аргументы, полный путь - в tim port.
func shortArgs(r PortRow) string {
	cmd, args := r.Proc.Command, cmp.Or(r.Args, r.Proc.Command)
	if rest, ok := strings.CutPrefix(args, cmd); ok && strings.Contains(cmd, "/") {
		return filepath.Base(cmd) + rest
	}
	return args
}

// forgottenWhy: почему процесс считается забытым.
func forgottenWhy(r PortRow) string {
	switch {
	case r.Agent != nil && !r.Agent.Alive:
		return "сессия агента, которая его подняла, закрыта"
	case r.Agent != nil && !r.Agent.Active.IsZero():
		return "агент молчит " + ui.Short(time.Since(r.Agent.Active)) + ", а процесс работает " + ui.Short(r.Uptime)
	}
	return "работает " + ui.Short(r.Uptime) + " - обычно такой забывают остановить"
}

// closeRow: остановить процесс строки - SIGTERM, принудительно - SIGKILL; контейнер - docker stop.
func closeRow(r PortRow, force bool) (string, error) {
	if r.Container != nil {
		_, err := runner.Quiet("docker", "stop", r.Container.Name)
		return r.Container.Name + ": остановлен", err
	}
	if r.Listener.PID == "" {
		return "", ui.Errorf("процесс чужого пользователя: sudo lsof -nP -iTCP:%s -sTCP:LISTEN", r.Port)
	}
	sig := "-TERM"
	if force {
		sig = "-KILL"
	}
	_, err := runner.Quiet("kill", sig, r.Listener.PID)
	return ui.Dash(r.Kind) + " :" + r.Port + " закрыт", err
}

// RunningScreen: Машина сразу на вкладке Запущенные.
func RunningScreen(env *app.Env) tui.Screen { return machineScreen(env, tabRunning, tui.ProcsOpts{}) }
