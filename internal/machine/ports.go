package machine

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// PortRow: слушающий порт, его процесс и проект. Проект с "?" - угадан по порту из реестра.
type PortRow struct {
	netinfo.Listener
	procs.Proc
	Project string
}

type portsOptions struct {
	all, old, kill, force bool
	hours                 float64
}

// Ports: кто слушает TCP-порты и чей это проект.
func Ports(cfg *config.Config) []PortRow {
	ls := netinfo.Listening()
	pids := make([]string, 0, len(ls))
	for _, l := range ls {
		pids = append(pids, l.PID)
	}
	info := procs.Info(pids)
	rows := make([]PortRow, 0, len(ls))
	for _, l := range ls {
		r := PortRow{Listener: l, Proc: info[l.PID]}
		if r.Proc.Command == "" {
			r.Proc.Command = l.Command
		}
		// Приложения из .app стартуют в папке, откуда их открыли, это не делает их частью проекта.
		if r.Cwd != "" && r.Cwd != "/" && !strings.Contains(r.Proc.Command, ".app/Contents/") {
			if p, ok := cfg.ProjectHere(r.Cwd); ok {
				r.Project = p.Name
			}
		}
		if r.Project == "" {
			for _, p := range cfg.Projects {
				if strconv.Itoa(p.Port) == l.Port {
					r.Project = p.Name + "?"
				}
			}
		}
		rows = append(rows, r)
	}
	return rows
}

// Forgotten: dev-серверы проектов, которые работают дольше limit: их обычно забыли остановить.
func Forgotten(rows []PortRow, limit time.Duration) []PortRow {
	var out []PortRow
	for _, r := range rows {
		if r.Project != "" && !strings.HasSuffix(r.Project, "?") && r.Uptime >= limit {
			out = append(out, r)
		}
	}
	return out
}

func printPorts(cfg *config.Config, o portsOptions) error {
	if o.old {
		return printForgotten(cfg, o)
	}
	var rows []PortRow
	for _, r := range Ports(cfg) {
		port, _ := strconv.Atoi(r.Port)
		if !o.all && r.Project == "" && (procs.System(r.Proc.Command) || port > 49151) {
			continue
		}
		rows = append(rows, r)
	}
	printRows(rows)
	if !o.all {
		ui.Println(ui.Dim("системные и эфемерные порты скрыты: --all"))
	}
	return nil
}

func printForgotten(cfg *config.Config, o portsOptions) error {
	rows := Forgotten(Ports(cfg), time.Duration(o.hours*float64(time.Hour)))
	if len(rows) == 0 {
		ui.Println(ui.Dim(fmt.Sprintf("процессов проектов старше %g ч нет", o.hours)))
		return nil
	}
	printRows(rows)
	if o.kill {
		killRows(rows, o.force)
		return nil
	}
	ui.Println(ui.Dim("остановить: tim ports --old --kill"))
	return nil
}

// portOwner: кто держит порт; с "kill" вторым словом - остановить его.
func portOwner(cfg *config.Config, args []string, force bool) error {
	port := args[0]
	if _, err := strconv.Atoi(port); err != nil {
		return ui.Errorf("порт должен быть числом: %s", port)
	}
	var found []PortRow
	for _, r := range Ports(cfg) {
		if r.Port == port {
			found = append(found, r)
		}
	}
	if len(found) == 0 {
		ui.Println(ui.Dim("порт " + port + " свободен"))
		return nil
	}
	printRows(found)
	if len(args) > 1 && args[1] == "kill" {
		killRows(found, force)
	}
	return nil
}

func killRows(rows []PortRow, force bool) {
	sig := "-TERM"
	if force {
		sig = "-KILL"
	}
	for _, r := range rows {
		if !runner.Confirm(fmt.Sprintf("остановить %s (pid %s)?", r.Proc.Command, r.Listener.PID)) {
			continue
		}
		if err := runner.Run("kill", sig, r.Listener.PID); err != nil {
			ui.Println(ui.Bad("не вышло: " + err.Error()))
		}
	}
}

func checkPorts(addrs []string) error {
	failed := 0
	for _, a := range addrs {
		d, err := netinfo.Dial(a, inspect.Timeout)
		if err != nil {
			failed++
			ui.Println(ui.Bad(ui.Pad(a, 28)) + " " + ui.Dim(err.Error()))
			continue
		}
		ui.Println(ui.Label(ui.Pad(a, 28)) + fmt.Sprintf(" открыт, %d мс", d.Milliseconds()))
	}
	if failed > 0 {
		return ui.Errorf("закрыто: %d из %d", failed, len(addrs))
	}
	return nil
}

func printRows(rows []PortRow) {
	ui.Println(ui.Dim(ui.Pad("ПОРТ", 6) + " " + ui.Pad("PID", 7) + " " + ui.Pad("РАБОТАЕТ", 12) + " " + ui.Pad("ПРОЕКТ", 18) + " КОМАНДА"))
	for _, r := range rows {
		cmd := r.Proc.Command
		if len([]rune(cmd)) > 60 {
			cmd = "..." + string([]rune(cmd)[len([]rune(cmd))-57:])
		}
		ui.Println(ui.Label(ui.Pad(r.Port, 6)) + " " + ui.Pad(r.Listener.PID, 7) + " " + ui.Pad(uptime(r.Uptime), 12) + " " +
			ui.Pad(ui.Dash(r.Project), 18) + " " + ui.Dim(cmd))
	}
}

func uptime(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	return ui.Duration(d)
}
