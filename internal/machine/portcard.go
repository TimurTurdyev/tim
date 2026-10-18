package machine

import (
	"cmp"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/gitx"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// shells: родитель-оболочка - значит, процесс запущен руками в терминале.
var shells = map[string]bool{"zsh": true, "bash": true, "fish": true, "sh": true, "-zsh": true, "-bash": true}

// printCard: всё о процессе на порту - что это, где, сколько ест, кто запустил и куда пишет.
// Строки без данных не печатаются.
func printCard(r PortRow) {
	title := ":" + strings.Join(r.Ports, ", :") + "  " + ui.Dash(r.Kind)
	if r.Project != "" {
		title += "  " + r.Project
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
	folder := paths.Show(r.Cwd)
	// Ветка - только у проекта: /opt/homebrew тоже репозиторий, и его ветка ничего не говорит.
	if st, ok := gitx.Status(r.Dir); r.Dir != "" && ok && st.Branch != "" {
		folder += ui.Dim("   ветка ") + st.Branch
	}
	line("папка", folder)
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
	who, log := starter(r)
	line("запустил", who)
	if log == "" {
		out, errs := procs.Outputs(r.Listener.PID)
		log = out
		if errs != "" && errs != out {
			log = strings.TrimSpace(out + "  ошибки: " + errs)
		}
	}
	line("журнал", paths.Show(log))
	ui.Println(ui.Dim("  остановить: tim port " + r.Port + " kill"))
}

// starter: кто запустил процесс и, если это tim serve, где его журнал. Сервер из tim serve - часто
// внук записанного процесса (sh -> php artisan serve -> php -S), поэтому предки проверяются на три шага.
func starter(r PortRow) (who, log string) {
	states := run.ListStates()
	pid, ppid := r.Listener.PID, r.PPID
	for range 3 {
		for _, s := range states {
			if strconv.Itoa(s.PID) == pid {
				return "tim serve " + s.Name, s.Log
			}
		}
		if ppid == "" || ppid == "0" || ppid == "1" {
			break
		}
		parent := procs.Info([]string{ppid})[ppid]
		pid, ppid = ppid, parent.PPID
		if parent.Command == "" {
			break
		}
		if name := filepath.Base(parent.Command); shells[name] {
			return "терминал (" + strings.TrimPrefix(name, "-") + " " + pid + ")", ""
		}
		if who == "" {
			who = shortCommand(parent) + " " + pid
		}
	}
	if r.PPID == "1" {
		return "без терминала, работает сам по себе", ""
	}
	return who, ""
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
