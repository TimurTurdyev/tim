package cmd

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

type portRow struct {
	netinfo.Listener
	procs.Proc
	project string
}

func listenRows() []portRow {
	ls := netinfo.Listening()
	pids := make([]string, 0, len(ls))
	for _, l := range ls {
		pids = append(pids, l.PID)
	}
	info := procs.Info(pids)
	rows := make([]portRow, 0, len(ls))
	for _, l := range ls {
		r := portRow{Listener: l, Proc: info[l.PID]}
		if r.Proc.Command == "" {
			r.Proc.Command = l.Command
		}
		// Приложения из .app стартуют в папке, откуда их открыли, это не делает их частью проекта.
		if r.Cwd != "" && r.Cwd != "/" && !strings.Contains(r.Proc.Command, ".app/Contents/") {
			if p, ok := cfg.ProjectHere(r.Cwd); ok {
				r.project = p.Name
			}
		}
		if r.project == "" {
			for _, p := range cfg.Projects {
				if strconv.Itoa(p.Port) == l.Port {
					r.project = p.Name + "?"
				}
			}
		}
		rows = append(rows, r)
	}
	return rows
}

func uptime(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	return ui.Duration(d)
}

func printRows(rows []portRow) {
	ui.Println(ui.Dim(ui.Pad("ПОРТ", 6) + " " + ui.Pad("PID", 7) + " " + ui.Pad("РАБОТАЕТ", 12) + " " + ui.Pad("ПРОЕКТ", 18) + " КОМАНДА"))
	for _, r := range rows {
		cmd := r.Proc.Command
		if len([]rune(cmd)) > 60 {
			cmd = "..." + string([]rune(cmd)[len([]rune(cmd))-57:])
		}
		ui.Println(ui.Label(ui.Pad(r.Port, 6)) + " " + ui.Pad(r.Listener.PID, 7) + " " + ui.Pad(uptime(r.Uptime), 12) + " " + ui.Pad(ui.Dash(r.project), 18) + " " + ui.Dim(cmd))
	}
}

var portsAll bool

var portsCmd = &cobra.Command{
	Use:   "ports [--all] [--old [--hours 8] [--kill]]",
	Short: "Кто слушает TCP-порты: процесс, сколько работает, чей проект",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if portsOld {
			return portsOldRun()
		}
		var rows []portRow
		for _, r := range listenRows() {
			port, _ := strconv.Atoi(r.Port)
			if !portsAll && r.project == "" && (procs.System(r.Proc.Command) || port > 49151) {
				continue
			}
			rows = append(rows, r)
		}
		printRows(rows)
		if !portsAll {
			ui.Println(ui.Dim("системные и эфемерные порты скрыты: --all"))
		}
		return nil
	},
}

var portForce bool

func portRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	port := args[0]
	if _, err := strconv.Atoi(port); err != nil {
		return ui.Errorf("порт должен быть числом: %s", port)
	}
	var found []portRow
	for _, r := range listenRows() {
		if r.Port == port {
			found = append(found, r)
		}
	}
	if len(found) == 0 {
		ui.Println(ui.Dim("порт " + port + " свободен"))
		return nil
	}
	printRows(found)
	if len(args) < 2 || args[1] != "kill" {
		return nil
	}
	return killRows(found)
}

func killRows(rows []portRow) error {
	sig := "-TERM"
	if portForce {
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
	return nil
}

var (
	staleHours float64
	staleKill  bool
	portsOld   bool
)

// portsOld: dev-серверы проектов, которые работают дольше --hours: их обычно забыли остановить.
func portsOldRun() error {
	limit := time.Duration(staleHours * float64(time.Hour))
	var rows []portRow
	for _, r := range listenRows() {
		if r.project != "" && !strings.HasSuffix(r.project, "?") && r.Uptime >= limit {
			rows = append(rows, r)
		}
	}
	if len(rows) == 0 {
		ui.Println(ui.Dim(fmt.Sprintf("процессов проектов старше %g ч нет", staleHours)))
		return nil
	}
	printRows(rows)
	if staleKill {
		return killRows(rows)
	}
	ui.Println(ui.Dim("остановить: tim ports --old --kill"))
	return nil
}

func init() {
	portsCmd.Flags().BoolVar(&portsAll, "all", false, "показать и системные процессы")
	portCmd.Use = "port <n> [kill] | port check <host:port...>"
	portCmd.Short = "Кто держит порт, остановить его; проверка чужих портов"
	portCmd.RunE = portRun
	portCmd.Flags().BoolVar(&portForce, "force", false, "SIGKILL вместо SIGTERM")
	portsCmd.Flags().BoolVar(&portsOld, "old", false, "dev-серверы проектов, которые работают дольше --hours")
	portsCmd.Flags().Float64Var(&staleHours, "hours", 8, "сколько часов считать долгим")
	portsCmd.Flags().BoolVar(&staleKill, "kill", false, "с --old: предложить остановить каждый")
	portsCmd.Flags().BoolVar(&portForce, "force", false, "SIGKILL вместо SIGTERM")
	root.AddCommand(portsCmd)
}
