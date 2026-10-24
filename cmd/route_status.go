package cmd

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// guardDir: где пишет состояние страж-служба. Она под root, поэтому не в домашней папке.
func guardDir() string {
	if runtime.GOOS == "darwin" {
		return "/var/db/tim"
	}
	return "/var/lib/tim"
}

// guardStatus: состояние службы, а если её нет - последнего ручного route watch --once.
func guardStatus() (routewatch.Status, bool) {
	if st, ok := routewatch.Load(guardDir()); ok {
		return st, true
	}
	return routewatch.Load(stateDir())
}

func guardInstalled() bool {
	path := routesPlist()
	if runtime.GOOS == "linux" {
		path = routesService()
	}
	_, err := os.Stat(path)
	return err == nil
}

func guardRunning() bool {
	switch runtime.GOOS {
	case "darwin":
		out, err := runner.Output("launchctl", "print", "system/"+routesLabel)
		return err == nil && strings.Contains(out, "state = running")
	case "linux":
		out, _ := runner.Output("systemctl", "is-active", routesUnit)
		return strings.TrimSpace(out) == "active"
	}
	return false
}

// guardLine: одна строка о страже для tim route и tim vpn.
func guardLine() string {
	if !guardInstalled() {
		return ui.Dim("страж не установлен: после смены сети обход сам не починится; sudo tim route install")
	}
	st, ok := guardStatus()
	if !guardRunning() {
		return ui.Bad("страж установлен, но не работает: tim route status")
	}
	if !ok {
		return ui.Dim("страж работает, отчёта пока нет")
	}
	age := time.Since(st.LastCheck)
	line := ui.Label("страж работает") + ui.Dim(fmt.Sprintf("  проверка %s назад, починок за сутки %d", ui.Duration(age), st.Since(time.Now().Add(-24*time.Hour))))
	if age > time.Minute {
		line = ui.Warn(fmt.Sprintf("страж молчит %s", ui.Duration(age))) + ui.Dim("  tim route status")
	}
	if st.Error != "" {
		line += "  " + ui.Warn(st.Error)
	}
	return line
}

var routeStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Как работает страж маршрутов: последняя проверка, починки и их причины",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		installed, running := guardInstalled(), guardRunning()
		switch {
		case !installed:
			ui.Println(ui.Warn("служба не установлена") + ui.Dim("  sudo tim route install"))
		case running:
			ui.Println(ui.Label("служба работает") + ui.Dim("  журнал "+routesLog()))
		default:
			ui.Println(ui.Bad("служба установлена, но не работает") + ui.Dim("  журнал "+routesLog()))
		}
		st, ok := guardStatus()
		if !ok {
			ui.Println(ui.Dim("отчёта стража нет"))
			return nil
		}
		now := time.Now()
		ui.Println(ui.Label("последняя проверка ") + ui.Duration(now.Sub(st.LastCheck)) + " назад" +
			ui.Dim(", запущен "+st.Started.Format("02.01 15:04")))
		ui.Println(ui.Label("сеть               ") + st.Iface + " шлюз " + ui.Dash(st.Gateway))
		ok2 := strconv.Itoa(st.OK) + " из " + strconv.Itoa(st.Want)
		if st.OK < st.Want {
			ok2 = ui.Warn(ok2)
		}
		ui.Println(ui.Label("маршрутов в порядке ") + ok2)
		if st.Error != "" {
			ui.Println(ui.Bad("ошибка: " + st.Error))
		}
		day, week := st.Since(now.Add(-24*time.Hour)), st.Since(now.AddDate(0, 0, -7))
		ui.Println(ui.Label("починок            ") + fmt.Sprintf("за сутки %d, за неделю %d", day, week))
		if len(st.Repairs) == 0 {
			return nil
		}
		var recent []routewatch.Repair
		for _, r := range st.Repairs {
			if r.At.After(now.AddDate(0, 0, -7)) {
				recent = append(recent, r)
			}
		}
		sum := routewatch.Summary(recent)
		kinds := make([]string, 0, len(sum))
		for k := range sum {
			kinds = append(kinds, k)
		}
		sort.Slice(kinds, func(i, j int) bool { return sum[kinds[i]] > sum[kinds[j]] })
		for _, k := range kinds {
			ui.Println(ui.Dim("  " + ui.Pad(strconv.Itoa(sum[k]), 5) + k))
		}
		ui.Println(ui.Dim("последние:"))
		from := max(0, len(st.Repairs)-5)
		for _, r := range st.Repairs[from:] {
			ui.Println(ui.Dim("  " + r.At.Format("02.01 15:04") + "  " + ui.Pad(r.Dest, 18) + " " + r.Reason))
		}
		return nil
	},
}

func init() {
	routeCmd.AddCommand(routeStatusCmd)
}
