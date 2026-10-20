package servers

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// printServer: отчёт о сервере строками или JSON.
func printServer(target string, asJSON bool) error {
	r, err := collect(target, 15*time.Second)
	if err != nil {
		return ui.Errorf("%s: %v; маршрут и вход проверит tim ssh %s", target, err, target)
	}
	findings := srvinfo.Evaluate(r, time.Now())
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(struct {
			Report   srvinfo.Report `json:"report"`
			Findings []health.Check `json:"findings"`
		}{r, findings})
	}
	ui.Println(ui.Label(target) + "  " + ui.Dim(summaryLine(r)))
	if len(findings) == 0 {
		ui.Println("  " + ui.Label("замечаний нет"))
		return nil
	}
	printFindings(findings)
	return nil
}

// collect: один заход по ssh, скрипт уходит через stdin, на сервере ничего не остаётся.
func collect(target string, timeout time.Duration) (srvinfo.Report, error) {
	out, err := runner.OutputStdin(srvinfo.Script, "ssh", "-o", "BatchMode=yes",
		"-o", "ConnectTimeout="+fmt.Sprint(int(timeout.Seconds())), target, "sh -s")
	if !strings.Contains(out, "@@end") {
		if err == nil {
			err = fmt.Errorf("скрипт не дошёл до конца")
		}
		return srvinfo.Report{}, err
	}
	return srvinfo.Parse(out), nil
}

// osLine: система и сколько работает - шапка экрана, где остальное видно полосами.
func osLine(r srvinfo.Report) string {
	var parts []string
	if r.OS != "" {
		parts = append(parts, r.OS)
	}
	if r.Uptime > 0 {
		parts = append(parts, "up "+ui.Duration(r.Uptime))
	}
	return strings.Join(parts, "  ")
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
	for _, d := range r.Disks {
		if d.Mount == "/" {
			parts = append(parts, fmt.Sprintf("диск %d%%", d.Pct))
		}
	}
	return strings.Join(parts, "  ")
}
