package machine

import (
	"cmp"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// shells: родитель-оболочка - значит, процесс запущен руками в терминале.
var shells = map[string]bool{"zsh": true, "bash": true, "fish": true, "sh": true, "-zsh": true, "-bash": true}

// printCard: всё о процессе на порту - что это, где, сколько ест, кто запустил и куда пишет.
// Строки без данных не печатаются.
func printCard(r PortRow) {
	title := ":" + strings.Join(r.Ports, ", :") + "  " + ui.Dash(r.Kind)
	if r.Folder != "" {
		title += "  " + r.Folder
	}
	ui.Println(ui.Label(title))
	line := func(k, v string) {
		if v != "" {
			ui.Println("  " + ui.Dim(ui.Pad(k, 10)) + v)
		}
	}
	switch {
	case r.Container != nil:
		line("контейнер", r.Container.Name+" ("+r.Container.Image+")")
		line("журнал", "docker logs -f "+r.Container.Name)
		ui.Println(ui.Dim("  остановить: docker stop " + r.Container.Name))
		return
	case r.Listener.PID == "":
		line("процесс", "чужого пользователя: sudo lsof -nP -iTCP:"+r.Port+" -sTCP:LISTEN")
		return
	}
	line("папка", paths.Show(r.Cwd))
	line("процесс", fmt.Sprintf("%s %s, работает %s, память %s", filepath.Base(r.Proc.Command), r.Listener.PID, uptime(r.Uptime), ui.Bytes(r.RSS)))
	width := 1 << 20
	if cols := ui.Cols(); cols > 0 {
		width = max(40, cols-14)
	}
	for i, l := range ui.Wrap(r.Args, width) {
		if i == 0 {
			line("команда", l)
		} else {
			ui.Println(strings.Repeat(" ", 12) + l)
		}
	}
	line("запустил", starter(r))
	out, errs := procs.Outputs(r.Listener.PID)
	log := out
	if errs != "" && errs != out {
		log = strings.TrimSpace(out + "  ошибки: " + errs)
	}
	line("журнал", paths.Show(log))
	ui.Println(ui.Dim("  остановить: tim port " + r.Port + " kill"))
}

// starter: кто запустил процесс - агент и его сессия, терминал или родитель. Сервер часто внук того, что
// запускали (sh -> php artisan serve -> php -S), поэтому предки проверяются на три шага.
func starter(r PortRow) string {
	if a := r.Agent; a != nil {
		return AgentText(*a)
	}
	var who, pid string
	ppid := r.PPID
	for range 3 {
		if ppid == "" || ppid == "0" || ppid == "1" {
			break
		}
		parent := procs.Info([]string{ppid})[ppid]
		pid, ppid = ppid, parent.PPID
		if parent.Command == "" {
			break
		}
		if name := filepath.Base(parent.Command); shells[name] {
			return "терминал (" + strings.TrimPrefix(name, "-") + " " + pid + ")"
		}
		if who == "" {
			who = shortCommand(parent) + " " + pid
		}
	}
	if r.PPID == "1" {
		return "без терминала, работает сам по себе"
	}
	return who
}

// AgentText: кто из агентов и в какой сессии запустил - имя, начало первой просьбы, открыта ли сессия.
func AgentText(a procs.Agent) string {
	out := a.Name
	if a.Title != "" {
		out += ", сессия «" + ui.Clip(a.Title, 48) + "»"
	} else if a.Session != "" {
		out += ", сессия " + a.Session[:min(8, len(a.Session))]
	}
	switch {
	case !a.Alive:
		out += ", закрыта"
	case !a.Active.IsZero():
		out += ", последний раз " + ui.Short(time.Since(a.Active)) + " назад"
	}
	return out
}

// shortCommand: начало команды без пути - php artisan serve, npm run dev.
func shortCommand(p procs.Proc) string {
	f := strings.Fields(cmp.Or(p.Args, p.Command))
	if len(f) == 0 {
		return ""
	}
	f[0] = filepath.Base(f[0])
	return strings.Join(f[:min(3, len(f))], " ")
}
