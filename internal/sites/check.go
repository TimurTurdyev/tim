package sites

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const checkLabel = "dev.tim.check"

// checkCols: колонки таблицы проверки и их ширина.
var checkCols = []struct {
	name  string
	width int
}{
	{"git", 26},
	{"сайт", 12},
	{"сертификат", 14},
	{"почта", 8},
	{"домен", 10},
	{"сервер", 20},
	{"секреты", 14},
	{"зависимости", 13},
}

// check: обход проектов. В терминале без аргументов - экран, иначе таблица и замечания о машине.
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
	case len(args) == 0 && !o.notify && !o.json && app.Interactive():
		return env.Open(checkScreen(env, o, true))
	}
	projects, err := projectsOrDomains(env.Cfg, args)
	if err != nil {
		return err
	}
	if len(projects) == 0 {
		return ui.Errorf("в реестре нет проектов: tim proj scan")
	}
	rows := checkRows(env.Cfg, projects, o)
	var domains, aliases []string
	if !o.local {
		domains = roundDomains(projects)
		watchDomains(domains)
	}
	if o.servers && !o.local {
		aliases = roundAliases(projects)
	}
	if len(rows) == 0 {
		return ui.Errorf("проверять нечего: ни у одного проекта нет git и адреса сайта (tim sync --remote)")
	}
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
		return ui.Errorf("требуют внимания: %d из %d", bad, len(rows))
	}
	return nil
}

// checkRows: проверки проектов параллельно; кому проверять нечего, в таблице только шумят.
func checkRows(cfg *config.Config, projects []*config.Project, o checkOptions) []health.Row {
	var shared DomainChecks
	rows := par.Map(projects, par.Limit, func(p *config.Project) health.Row {
		return CheckRow(p, cfg.ProjectDir(p), o.local, o.net, o.servers, &shared)
	})
	kept := rows[:0]
	for _, r := range rows {
		if r.Level() != level.Skip {
			kept = append(kept, r)
		}
	}
	health.Sort(kept)
	var registry []health.Row
	for _, r := range kept {
		if _, ok := cfg.Project(r.Name); ok {
			registry = append(registry, r)
		}
	}
	health.SaveRows(registry)
	return kept
}

// projectsOrDomains: проекты для обхода; без аргументов - весь реестр. Домен, которого нет в реестре,
// проверяется как есть: сайт, сертификат, почта, регистрация.
func projectsOrDomains(cfg *config.Config, args []string) ([]*config.Project, error) {
	if len(args) == 0 {
		out := make([]*config.Project, 0, len(cfg.Projects))
		for i := range cfg.Projects {
			out = append(out, &cfg.Projects[i])
		}
		return out, nil
	}
	var out []*config.Project
	for _, name := range args {
		p, err := cfg.Pick(name)
		if err == nil {
			out = append(out, p)
			continue
		}
		if !strings.Contains(name, ".") || strings.Contains(name, "/") {
			return nil, err
		}
		host := strings.TrimPrefix(strings.TrimPrefix(name, "https://"), "http://")
		out = append(out, &config.Project{Name: name, URL: "https://" + host})
	}
	return out, nil
}

func printCheck(rows []health.Row) {
	cols := usedCols(rows)
	head := ui.Pad("проект", 22)
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
		}
	}
}

// usedCols: колонки, в которых есть что показать хоть одному проекту.
func usedCols(rows []health.Row) []struct {
	name  string
	width int
} {
	cols := checkCols[:0:0]
	for _, col := range checkCols {
		for _, r := range rows {
			if lvl := r.Get(col.name).Level; lvl != level.Skip && (lvl >= level.Warn || !slices.Contains(quietCols[1:], col.name)) {
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

// machineNotes: замечания не о проектах, а о самой машине.
func machineNotes(cfg *config.Config, older time.Duration) []string {
	out := machine.HogLines("")
	var forgotten []string
	for _, r := range machine.Forgotten(machine.Ports(cfg), older) {
		forgotten = append(forgotten, r.Project+":"+r.Port)
	}
	if len(forgotten) > 0 {
		out = append(out, ui.Warn("dev-серверы старше "+ui.Short(older)+": ")+strings.Join(forgotten, " ")+ui.Dim("  (tim ports --old --kill)"))
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
		Log: filepath.Join(filepath.Dir(run.StateDir()), "check.log"), Env: schedule.JobEnv()}
	if err := schedule.Install(job, "tim-check"); err != nil {
		return err
	}
	ui.Println(ui.Label("обход каждый день в " + at))
	ui.Println(ui.Dim("уведомление придёт только о новом и серьёзном; Telegram и ntfy - [notify] в конфиге, tim notify test; журнал: " + job.Log))
	return nil
}
