package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var serverJSON bool

// serverTarget: алиас, проект с полем ssh или user@host как есть.
func serverTarget(arg string) string {
	if p, ok := cfg.Project(arg); ok && p.SSH != "" {
		return p.SSH
	}
	return arg
}

// collectServer: один заход по ssh, скрипт уходит через stdin, на сервере ничего не остаётся.
func collectServer(target string, timeout time.Duration) (srvinfo.Report, error) {
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

func summaryLine(r srvinfo.Report) string {
	var parts []string
	if r.OS != "" {
		parts = append(parts, r.OS)
	}
	if r.Uptime > 0 {
		parts = append(parts, "up "+ui.Duration(r.Uptime))
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

func printChecks(list []health.Check) {
	if len(list) == 0 {
		ui.Println("  " + ui.Label("замечаний нет"))
		return
	}
	for _, c := range list {
		text := ui.Pad(c.Name, 14) + c.Detail
		switch c.Level {
		case health.Bad:
			ui.Println("  " + ui.Bad(text))
		case health.Warn:
			ui.Println("  " + ui.Warn(text))
		default:
			ui.Println("  " + ui.Dim(text))
		}
	}
}

var serverCmd = &cobra.Command{
	Use:   "server <алиас|проект|user@host> [--json]",
	Short: "Состояние Linux-сервера по ssh: место, память, юниты, обновления, порты наружу, sshd",
	Long: `Один заход по ssh, только чтение: скрипт уходит через stdin и ничего не ставит и не меняет.
То, что требует root (sshd -T, nginx -t, сертификаты), идёт через sudo -n: без пароля он не спрашивает,
а отказывает, и такие проверки помечаются "не проверено".`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := serverTarget(args[0])
		if tui.Available() && !serverJSON {
			return runScreen(serverScreen(target))
		}
		r, err := collectServer(target, 15*time.Second)
		if err != nil {
			return ui.Errorf("%s: %v; маршрут и вход проверит tim ssh %s", target, err, target)
		}
		findings := srvinfo.Evaluate(r, time.Now())
		if serverJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(struct {
				Report   srvinfo.Report `json:"report"`
				Findings []health.Check `json:"findings"`
			}{r, findings})
		}
		ui.Println(ui.Label(target) + "  " + ui.Dim(summaryLine(r)))
		printChecks(findings)
		return nil
	},
}

func init() {
	serverCmd.Flags().BoolVar(&serverJSON, "json", false, "отчёт в JSON")
	root.AddCommand(serverCmd)
}
