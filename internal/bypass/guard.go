package bypass

import (
	"fmt"
	"maps"
	"os"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	GuardLabel = "dev.tim.routes"
	GuardUnit  = "tim-routes"
)

func GuardPlist() string   { return schedule.PlistPath(GuardLabel, true) }
func GuardService() string { return "/etc/systemd/system/" + GuardUnit + ".service" }
func GuardLog() string     { return "/var/log/tim-routes.log" }

// GuardDir: где пишет состояние страж-служба. Она под root, поэтому не в домашней папке.
func GuardDir() string { return paths.SystemState() }

// GuardStatus: состояние службы, а если её нет - последнего ручного route watch --once.
func GuardStatus() (routewatch.Status, bool) {
	if st, ok := routewatch.Load(GuardDir()); ok {
		return st, true
	}
	return routewatch.Load(StateDir())
}

func GuardInstalled() bool {
	path := GuardPlist()
	if runtime.GOOS == "linux" {
		path = GuardService()
	}
	_, err := os.Stat(path)
	return err == nil
}

func GuardRunning() bool {
	switch runtime.GOOS {
	case "darwin":
		out, err := runner.Output("launchctl", "print", "system/"+GuardLabel)
		return err == nil && strings.Contains(out, "state = running")
	case "linux":
		out, _ := runner.Output("systemctl", "is-active", GuardUnit)
		return strings.TrimSpace(out) == "active"
	}
	return false
}

// GuardLine: одна строка о страже для tim route и tim vpn.
func GuardLine() string {
	if !GuardInstalled() {
		return ui.Dim("страж не установлен: после смены сети обход сам не починится; sudo tim route install")
	}
	st, ok := GuardStatus()
	if !GuardRunning() {
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

// WantedDests: все адреса и сети целей конфига. Конфиг перечитывается каждый цикл, чтобы
// tim route add и rm действовали на стража без перезапуска.
func WantedDests(c *config.Config) []string {
	return flatten(ExpandHosts(c.Hosts))
}

func LogLine(format string, a ...any) {
	ui.Println(time.Now().Format("2006-01-02 15:04:05") + " " + fmt.Sprintf(format, a...))
}

// Cycle: одна проверка и починка по целям конфига. Возвращает починенное.
func Cycle(st *routewatch.Status, c *config.Config) []routewatch.Repair {
	st.LastCheck = time.Now()
	phys, gw := netinfo.Physical()
	st.Gateway, st.Iface = gw, phys.Device
	if phys.Device == "" || gw == "" {
		st.Error = "физический интерфейс или шлюз не найден: сети нет?"
		return nil
	}
	st.Error = ""
	want := WantedDests(c)
	repairs := routewatch.Decide(want, netinfo.Table(), gw, phys.Device)
	st.Want = len(uniq(want))
	st.OK = st.Want - len(repairs)
	var done []routewatch.Repair
	defer TableChanged()
	for _, r := range repairs {
		if err := setRoute(r.Dest, gw, phys.Device); err != nil {
			LogLine("%s: не удалось (%s): %v", r.Dest, r.Reason, err)
			continue
		}
		r.At = time.Now()
		done = append(done, r)
		st.OK++
	}
	if len(done) > 0 {
		sum := routewatch.Summary(done)
		var parts []string
		for _, k := range slices.Sorted(maps.Keys(sum)) {
			parts = append(parts, fmt.Sprintf("%s: %d", k, sum[k]))
		}
		LogLine("починено %d через %s %s (%s)", len(done), phys.Device, gw, strings.Join(parts, ", "))
	}
	st.AddRepairs(done)
	return done
}

func uniq(list []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range list {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
