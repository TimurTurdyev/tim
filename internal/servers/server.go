package servers

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/stats"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// serverTimeout: сколько ждать ssh и скрипт отчёта.
const serverTimeout = 15 * time.Second

// printServer: отчёт о сервере строками или JSON.
func printServer(target string, asJSON bool) error {
	r, err := srvinfo.Collect(target, serverTimeout)
	if err != nil {
		return ui.Errorf("%s: %v; маршрут и вход проверит tim ssh %s", target, err, target)
	}
	findings := evaluate(target, r)
	save(serverState{Target: target, Report: r, Findings: findings})
	if asJSON {
		return ui.JSON(struct {
			Report   srvinfo.Report `json:"report"`
			Findings []health.Check `json:"findings"`
		}{r, findings})
	}
	ui.Println(ui.Label(target) + "  " + ui.Dim(summaryLine(r)))
	if text, lvl, ok := forecast(target); ok && lvl == level.OK {
		ui.Println("  " + ui.Pad("прогноз", 14) + text)
	}
	if len(findings) == 0 {
		ui.Println("  " + ui.Label("замечаний нет"))
		return nil
	}
	printFindings(findings)
	return nil
}

// evaluate: находки по отчёту и по журналу замеров - если место кончается, это тоже находка;
// спокойный прогноз остаётся строкой сводки.
func evaluate(target string, r srvinfo.Report) []health.Check {
	findings := srvinfo.Evaluate(r, time.Now())
	if text, lvl, ok := forecast(target); ok && lvl >= level.Warn {
		findings = append(findings, health.Check{Name: "диск", Level: lvl, Text: "кончается", Detail: text,
			Fix: "tim server " + target + " и du -xsh /var/* /home/* на сервере: что растёт"})
		slices.SortStableFunc(findings, func(a, b health.Check) int { return int(b.Level) - int(a.Level) })
	}
	return findings
}

// forecast: прогноз места по журналу замеров сервера; ok = false, если замеров нет или мало.
func forecast(target string) (string, level.Level, bool) {
	list := stats.Load(journal(target))
	if len(list) == 0 {
		return "", level.OK, false
	}
	return stats.ForecastLine(list)
}

// journal: журнал замеров по хосту, а не по алиасу - под root и под www это один сервер.
func journal(target string) string {
	t := sshx.Resolve(target)
	switch {
	case t.Host == "":
		return stats.Path(target)
	case t.Port != "" && t.Port != "22":
		return stats.Path(t.Host + "_" + t.Port)
	}
	return stats.Path(t.Host)
}

// sshError: что сказать вместо кода выхода ssh.
func sshError(err error) string {
	if strings.Contains(err.Error(), "255") {
		return "ssh не подключился: имя не резолвится, порт закрыт или ключ не подходит"
	}
	return err.Error()
}

// osLine: система и сколько работает - шапка экрана, где остальное видно полосами.
func osLine(r srvinfo.Report) string {
	var parts []string
	if r.OS != "" {
		parts = append(parts, r.OS)
	}
	if r.Uptime > 0 {
		parts = append(parts, "работает "+ui.Short(r.Uptime))
	}
	return strings.Join(parts, " \u00b7 ")
}

// printFindings: находки строками, у настоящих бед - с чего начать.
func printFindings(findings []health.Check) {
	for _, c := range findings {
		text := ui.Pad(c.Name, 14) + c.Detail
		switch c.Level {
		case level.Bad:
			ui.Println("  " + ui.Bad(text))
		case level.Warn:
			ui.Println("  " + ui.Warn(text))
		default:
			ui.Println("  " + ui.Dim(text))
		}
		if c.Fix != "" && c.Level >= level.Warn {
			ui.Println(ui.Dim("    \u2192 " + c.Fix))
		}
	}
}

func summaryLine(r srvinfo.Report) string {
	var parts []string
	if l := osLine(r); l != "" {
		parts = append(parts, l)
	}
	if r.Cores > 0 {
		parts = append(parts, fmt.Sprintf("load %.2f, ядер %d", r.Load[1], r.Cores))
	}
	if r.MemTotal > 0 {
		parts = append(parts, "память "+ui.Bytes(r.MemTotal-r.MemAvail)+" из "+ui.Bytes(r.MemTotal))
	}
	if d, ok := r.Root(); ok {
		parts = append(parts, fmt.Sprintf("диск %d%%", d.Pct))
	}
	return strings.Join(parts, "  ")
}
