package sites

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/servers"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const checkLabel = "dev.tim.check"

// checkCols: колонки таблицы обхода и их ширина.
var checkCols = []struct {
	name  string
	width int
}{
	{"сайт", 12},
	{"сертификат", 14},
	{"почта", 8},
	{"домен", 10},
	{"сервер", 20},
}

// check: обход сайтов, а с --servers - и серверов. В терминале без аргументов - экран Сайтов, иначе
// таблица и замечания о машине.
func check(env *app.Env, args []string, o checkOptions) error {
	switch {
	case o.machine:
		return machine.PrintProtection(env.Cfg)
	case o.install:
		return installCheck(o.at, o.servers)
	case o.uninstall:
		if err := schedule.Uninstall(checkLabel, "tim-check", false); err != nil {
			return err
		}
		ui.Println(ui.Label("ежедневный обход убран"))
		return nil
	case len(args) == 0 && !o.notify && !o.json && !o.servers && app.Interactive():
		return env.Open(SitesScreen(env))
	}
	hosts := siteHosts(env.Cfg, args)
	var aliases []string
	if o.servers {
		for _, t := range servers.Targets(env, nil) {
			aliases = append(aliases, t.Alias)
		}
	}
	if len(hosts) == 0 && len(aliases) == 0 {
		return ui.Errorf("проверять нечего: сайтов нет - добавь их в конфиг: sites = [\"example.com\"]; серверы - tim check --servers")
	}
	rows := checkRows(hosts, aliases)
	domains := roundDomains(hosts)
	watchDomains(domains)
	if o.json {
		return ui.JSON(rows)
	}
	printCheck(rows)
	if o.notify {
		notifyRound(env.Cfg, rows, domains, aliases)
	}
	ok, warn, bad := health.Counts(rows)
	ui.Println(ui.Dim(fmt.Sprintf("всего %d: ок %d, внимание %d", len(rows), ok, warn)))
	if lines := machineNotes(env.Cfg, o.older); len(lines) > 0 {
		ui.Println("")
		for _, l := range lines {
			ui.Println(l)
		}
	}
	if bad > 0 {
		return ui.Verdictf("требуют внимания: %d из %d", bad, len(rows))
	}
	return nil
}

// siteHosts: сайты обхода - аргументы как есть или список из конфига; локальные имена не проверяются.
func siteHosts(cfg *config.Config, args []string) []string {
	list := args
	if len(list) == 0 {
		list = cfg.SiteList()
	}
	var out []string
	for _, a := range list {
		host, _ := netinfo.HostPort(a, "")
		if host != "" && !localHost(host) && !slices.Contains(out, host) {
			out = append(out, host)
		}
	}
	return out
}

// checkRows: сайты и серверы параллельно; итог сайтов - в кэш для Сайтов и главного.
func checkRows(hosts, aliases []string) []health.Row {
	var shared DomainChecks
	rows := par.Map(hosts, par.Limit, func(h string) health.Row { return SiteRow(h, &shared) })
	rows = append(rows, par.Map(aliases, 6, ServerRow)...)
	health.Sort(rows)
	health.SaveRows(rows)
	return rows
}

func printCheck(rows []health.Row) {
	cols := usedCols(rows)
	head := ui.Pad("сайт или сервер", 22)
	for _, col := range cols {
		head += ui.Pad(col.name, col.width)
	}
	ui.Println(ui.Dim(head))
	for _, r := range rows {
		line := ui.Pad(r.Name, 22)
		for _, col := range cols {
			c := r.Get(col.name)
			line += ui.Pad(paint(c.Level, ui.Clip(c.Text, col.width-1)), col.width)
		}
		ui.Println(strings.TrimRight(line, " "))
		for _, c := range r.Problems() {
			ui.Println("  " + ui.Dim(c.Detail))
			if c.Fix != "" {
				ui.Println("    " + ui.Dim("→ "+c.Fix))
			}
		}
	}
}

// usedCols: колонки, в которых есть что показать хоть одной строке.
func usedCols(rows []health.Row) []struct {
	name  string
	width int
} {
	cols := checkCols[:0:0]
	for _, col := range checkCols {
		for _, r := range rows {
			if r.Get(col.name).Level != level.Skip {
				cols = append(cols, col)
				break
			}
		}
	}
	return cols
}

func paint(l level.Level, text string) string {
	switch l {
	case level.Bad:
		return ui.Bad(text)
	case level.Warn:
		return ui.Warn(text)
	case level.Skip:
		return ui.Dim(text)
	}
	return text
}

// machineNotes: замечания не о сайтах, а о самой машине: прожорливые и забытые процессы, диск.
func machineNotes(cfg *config.Config, older time.Duration) []string {
	out := machine.HogLines("")
	var forgotten []string
	for _, r := range machine.Forgotten(machine.PortsFull(cfg), older) {
		forgotten = append(forgotten, cmp.Or(r.Folder, r.Kind)+":"+r.Port)
	}
	if len(forgotten) > 0 {
		out = append(out, ui.Warn("забытые процессы: ")+strings.Join(forgotten, " ")+ui.Dim("  (tim ports --old --kill)"))
	}
	if d, ok := sysinfo.DataDisk(); ok && d.Percent() >= 85 {
		out = append(out, ui.Warn(fmt.Sprintf("диск занят на %d%%", d.Percent()))+ui.Dim("  (tim disk)"))
	}
	return out
}

func installCheck(at string, servers bool) error {
	self, err := schedule.SelfPath()
	if err != nil {
		return err
	}
	h, m, err := schedule.ParseAt(at)
	if err != nil {
		return ui.Errorf("%v", err)
	}
	args := []string{self, "check", "--notify"}
	if servers {
		args = append(args, "--servers")
	}
	job := schedule.Job{Label: checkLabel, Args: args, Hour: h, Minute: m,
		Log: paths.State("check.log"), Env: schedule.JobEnv()}
	if err := schedule.Install(job, "tim-check"); err != nil {
		return err
	}
	ui.Println(ui.Label("обход каждый день в " + at))
	ui.Println(ui.Dim("уведомление придёт только о новом и серьёзном; Telegram и ntfy - [notify] в конфиге, tim notify test; журнал: " + job.Log))
	return nil
}
