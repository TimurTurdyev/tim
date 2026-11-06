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
	"github.com/TimurTurdyev/tim/report"
)

// shells: родитель-оболочка - значит, процесс запущен руками в терминале.
var shells = map[string]bool{"zsh": true, "bash": true, "fish": true, "sh": true, "-zsh": true, "-bash": true}

// cardsReport: всё о процессах на порту - что это, где, сколько ест, кто запустил и куда пишет.
// Строки без данных не показываются.
func cardsReport(rows []PortRow) report.Report {
	r := report.Report{Data: portsData(rows)}
	for _, row := range rows {
		r.Blocks = append(r.Blocks, cardBlocks(row)...)
	}
	return r
}

func cardBlocks(r PortRow) []report.Block {
	title := ":" + strings.Join(r.Ports, ", :") + "  " + ui.Dash(r.Kind)
	if r.Folder != "" {
		title += "  " + r.Folder
	}
	var facts report.Facts
	add := func(k, v string) {
		if v != "" {
			facts = append(facts, report.Fact{Key: k, Value: v})
		}
	}
	switch {
	case r.Container != nil:
		add("контейнер", r.Container.Name+" ("+r.Container.Image+")")
		add("журнал", "docker logs -f "+r.Container.Name)
		return []report.Block{report.Heading(title), facts, report.Next{{Text: "остановить", Command: "docker stop " + r.Container.Name}}}
	case r.Listener.PID == "":
		add("процесс", "чужого пользователя: sudo lsof -nP -iTCP:"+r.Port+" -sTCP:LISTEN")
		return []report.Block{report.Heading(title), facts}
	}
	add("папка", paths.Show(r.Cwd))
	add("процесс", fmt.Sprintf("%s %s, работает %s, память %s", filepath.Base(r.Proc.Command), r.Listener.PID, uptime(r.Uptime), ui.Bytes(r.RSS)))
	add("команда", r.Args)
	add("запустил", starter(r))
	out, errs := procs.Outputs(r.Listener.PID)
	log := out
	if errs != "" && errs != out {
		log = strings.TrimSpace(out + "  ошибки: " + errs)
	}
	add("журнал", paths.Show(log))
	return []report.Block{report.Heading(title), facts, report.Next{{Text: "остановить", Command: "tim port " + r.Port + " kill"}}}
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
