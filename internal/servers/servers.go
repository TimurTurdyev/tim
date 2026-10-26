package servers

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/stats"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Все серверы разом: по 6 за раз. Замер по расписанию раз в 6 часов - чаще незачем, а каждый заход
// остаётся в журнале входов сервера.
const (
	serversLabel = "dev.tim.servers"
	serversLimit = 6
	serversEvery = 6 * 60 * 60
)

// serversOptions: какие серверы и что с ними.
type serversOptions struct{ all, json, record, install, uninstall bool }

// serverState: один сервер в сводке - отчёт и находки или почему не достучались.
type serverState struct {
	Target   string         `json:"target"`
	Projects []string       `json:"projects,omitempty"`
	Report   srvinfo.Report `json:"report"`
	Findings []health.Check `json:"findings"`
	Err      string         `json:"error,omitempty"`
	// Remembered: первый сбор запомнил всё своим - "запомнил 14 ключей ssh, 9 портов".
	Remembered string `json:"remembered,omitempty"`
}

func (s serverState) level() level.Level {
	if s.Err != "" {
		return level.Bad
	}
	worst := level.OK
	for _, c := range s.Findings {
		worst = max(worst, c.Level)
	}
	return worst
}

// state: короткая ячейка таблицы - худшая находка и сколько ещё.
func (s serverState) state() string {
	switch {
	case s.Err != "":
		return "не достучался"
	case len(s.Findings) == 0:
		return "в порядке"
	}
	problems := 0
	for _, c := range s.Findings {
		if c.Level >= level.Warn {
			problems++
		}
	}
	if problems == 0 {
		return "в порядке"
	}
	text := s.Findings[0].Name + " " + s.Findings[0].Text
	if problems > 1 {
		text += fmt.Sprintf(" +%d", problems-1)
	}
	return text
}

// runServers: сводка по серверам, замер в журнал или задание на замер.
func runServers(env *app.Env, args []string, o serversOptions) error {
	switch {
	case o.install:
		return installServers()
	case o.uninstall:
		if err := schedule.Uninstall(serversLabel, "tim-servers", false); err != nil {
			return err
		}
		ui.Println(ui.Label("замер серверов убран") + ui.Dim("  журналы остались в "+stats.Dir()))
		return nil
	}
	targets := Targets(env, args, o.all)
	if len(targets) == 0 {
		return ui.Errorf("серверов нет: у проектов не задано поле ssh (tim sync или конфиг); --all берёт все алиасы ~/.ssh/config")
	}
	if o.record {
		return recordServers(targets)
	}
	if app.Interactive() && !o.json {
		return env.Open(ServersScreen(env, targets))
	}
	states := collectAll(targets)
	if o.json {
		return ui.JSON(states)
	}
	printServers(states)
	return nil
}

// Server: сервер сводки - алиас, под которым заходим, и проекты, которые на нём живут.
type Server struct {
	Alias    string
	Projects []string
}

// hostKey: один хост под разными алиасами и пользователями - один ключ кэша.
func hostKey(alias string) string {
	t := sshx.Resolve(alias)
	if t.Host == "" {
		return alias
	}
	return t.Host + "-" + t.Port
}

// Targets: аргументы как есть, иначе ssh проектов реестра; all - ещё все алиасы ~/.ssh/config,
// кроме git-хостов. Один хост под разными пользователями - один сервер.
func Targets(env *app.Env, args []string, all bool) []Server {
	var aliases []string
	add := func(t string) {
		if t != "" && !slices.Contains(aliases, t) {
			aliases = append(aliases, t)
		}
	}
	for _, a := range args {
		add(Target(env.Cfg, a))
	}
	if len(args) == 0 {
		for _, p := range env.Cfg.Projects {
			add(p.SSH)
		}
		if all {
			for _, a := range sshx.Aliases() {
				if !gitHost(a) {
					add(a)
				}
			}
		}
		sort.Strings(aliases)
	}
	return byHost(env, aliases)
}

// byHost: алиасы одного хоста схлопываются в один сервер; заходим под root, если такой алиас есть, -
// у него отчёт полный, без "не проверено".
func byHost(env *app.Env, aliases []string) []Server {
	resolved := par.Map(aliases, serversLimit, sshx.Resolve)
	index := map[string]int{}
	var out []Server
	for i, a := range aliases {
		t := resolved[i]
		key := t.Host + ":" + t.Port
		if t.Host == "" {
			key = a
		}
		n, ok := index[key]
		if !ok {
			index[key] = len(out)
			out = append(out, Server{Alias: a, Projects: env.ProjectsOn(a)})
			continue
		}
		out[n].Projects = append(out[n].Projects, env.ProjectsOn(a)...)
		if t.User == "root" && resolved[slices.Index(aliases, out[n].Alias)].User != "root" {
			out[n].Alias = a
		}
	}
	preferRoot(out, aliases, resolved)
	return out
}

// preferRoot: если в ~/.ssh/config есть алиас root на тот же хост, сервер зовётся им - под root отчёт
// полный, без "не проверено".
func preferRoot(out []Server, aliases []string, resolved []sshx.Target) {
	others := slices.DeleteFunc(sshx.Aliases(), func(a string) bool { return slices.Contains(aliases, a) || gitHost(a) })
	extra := par.Map(others, serversLimit, sshx.Resolve)
	for i := range out {
		cur := resolved[slices.Index(aliases, out[i].Alias)]
		if cur.User == "root" || cur.Host == "" {
			continue
		}
		for j, t := range extra {
			if t.User == "root" && t.Host == cur.Host && t.Port == cur.Port {
				out[i].Alias = others[j]
				break
			}
		}
	}
}

func gitHost(alias string) bool {
	return alias == "github.com" || strings.HasPrefix(alias, "git.")
}

// collectAll: отчёты параллельно, худшие первыми.
func collectAll(targets []Server) []serverState {
	states := par.Map(targets, serversLimit, collect)
	sort.SliceStable(states, func(i, j int) bool { return states[i].level() > states[j].level() })
	return states
}

// printServers: таблица и под строкой - её беды.
func printServers(states []serverState) {
	ui.Println(ui.Dim(serversHeader()))
	for _, s := range states {
		ui.Println(strings.TrimRight(serverLine(s), " "))
		for _, c := range problems(s) {
			ui.Println("  " + ui.Dim(ui.Pad(c.Name, 14)+c.Detail))
		}
	}
	var ok, warn, bad int
	for _, s := range states {
		switch s.level() {
		case level.Bad:
			bad++
		case level.Warn:
			warn++
		default:
			ok++
		}
	}
	ui.Println(ui.Dim(fmt.Sprintf("всего %d: ок %d, внимание %d, плохо %d", len(states), ok, warn, bad)))
}

func serversHeader() string {
	return ui.Pad("сервер", 16) + ui.Pad("состояние", 30) + ui.Pad("up", 7) + ui.Pad("нагрузка", 10) + ui.Pad("память", 8) + ui.Pad("диск", 6) + "проекты"
}

// serverLine: строка таблицы; у недоступного сервера цифр нет.
func serverLine(s serverState) string {
	line := ui.Pad(ui.Clip(s.Target, 15), 16) + ui.Pad(level.Lamp(s.level(), ui.Clip(s.state(), 27)), 30)
	if s.Err != "" {
		return line + ui.Pad("", 31) + ui.Dim(strings.Join(s.Projects, ", "))
	}
	r := s.Report
	cell := func(pct int, ok bool, width int) string {
		if !ok {
			return ui.Pad("-", width)
		}
		return ui.Pad(ui.Percent(pct), width)
	}
	line += ui.Pad(ui.Short(r.Uptime), 7)
	line += cell(int(r.Load[1]*100/max(float64(r.Cores), 1)), r.Cores > 0, 10)
	line += cell(int((r.MemTotal-r.MemAvail)*100/max(r.MemTotal, 1)), r.MemTotal > 0, 8)
	d, ok := r.Root()
	line += cell(d.Pct, ok, 6)
	return line + ui.Dim(strings.Join(s.Projects, ", "))
}

// problems: что печатать под строкой таблицы - только настоящие беды, остальное покажет tim server.
func problems(s serverState) []health.Check {
	var out []health.Check
	for _, c := range s.Findings {
		if c.Level == level.Bad && len(out) < 3 {
			out = append(out, c)
		}
	}
	return out
}

// recordServers: замер каждого доступного сервера в его журнал.
func recordServers(targets []Server) error {
	var errs []string
	done := 0
	for _, s := range collectAll(targets) {
		if s.Err != "" {
			errs = append(errs, s.Target+": "+s.Err)
			continue
		}
		if err := stats.Append(journal(s.Target), sampleOf(s.Report)); err != nil {
			errs = append(errs, s.Target+": "+err.Error())
			continue
		}
		done++
	}
	ui.Println(ui.Label(fmt.Sprintf("замер %d из %d", done, len(targets))) + ui.Dim("  журналы в "+stats.Dir()))
	if len(errs) > 0 {
		return ui.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

// sampleOf: замер из отчёта - то же, что stats пишет про эту машину.
func sampleOf(r srvinfo.Report) stats.Sample {
	s := stats.Sample{At: time.Now(), Load: r.Load[0]}
	if r.MemTotal > 0 {
		s.MemUsed, s.MemTotal = r.MemTotal-r.MemAvail, r.MemTotal
	}
	if d, ok := r.Root(); ok {
		s.DiskTotal, s.DiskFree = d.Size, d.Size-d.Used
	}
	return s
}

func installServers() error {
	self, err := schedule.SelfPath()
	if err != nil {
		return err
	}
	job := schedule.Job{Label: serversLabel, Args: []string{self, "servers", "record"}, Every: serversEvery,
		Env: schedule.JobEnv(), Log: stats.Dir() + "/servers.log"}
	if err := schedule.Install(job, "tim-servers"); err != nil {
		return err
	}
	ui.Println(ui.Label("замер серверов каждые 6 часов") + ui.Dim("  журналы в "+stats.Dir()))
	ui.Println(ui.Dim("ssh должен входить без вопросов: ключ без пароля или в keychain; что пошло не так - в " + job.Log))
	return nil
}
