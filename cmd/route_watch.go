package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	watchRouteEvery time.Duration
	watchRouteOnce  bool
)

// wantedDests: все адреса и сети целей конфига. Конфиг перечитывается каждый цикл, чтобы
// tim route add и rm действовали на стража без перезапуска.
func wantedDests(c *config.Config) []string {
	var out []string
	for _, h := range c.Hosts {
		out = append(out, expandAddr(h.Addr)...)
	}
	return out
}

func logLine(format string, a ...any) {
	ui.Println(time.Now().Format("2006-01-02 15:04:05") + " " + fmt.Sprintf(format, a...))
}

// guardCycle: одна проверка и починка. Возвращает починенное.
func guardCycle(st *routewatch.Status) []routewatch.Repair {
	st.LastCheck = time.Now()
	c, err := config.Load()
	if err != nil {
		st.Error = "конфиг: " + err.Error()
		return nil
	}
	cfg = c
	phys, gw := physical()
	st.Gateway, st.Iface = gw, phys.Device
	if phys.Device == "" || gw == "" {
		st.Error = "физический интерфейс или шлюз не найден: сети нет?"
		return nil
	}
	st.Error = ""
	want := wantedDests(c)
	repairs := routewatch.Decide(want, netinfo.Table(), gw, phys.Device)
	st.Want = len(uniqStrings(want))
	st.OK = st.Want - len(repairs)
	var done []routewatch.Repair
	for _, r := range repairs {
		if err := setRoute(r.Dest, gw, phys.Device); err != nil {
			logLine("%s: не удалось (%s): %v", r.Dest, r.Reason, err)
			continue
		}
		r.At = time.Now()
		done = append(done, r)
		st.OK++
	}
	if len(done) > 0 {
		reasons := routewatch.Summary(done)
		var parts []string
		for k, n := range reasons {
			parts = append(parts, fmt.Sprintf("%s: %d", k, n))
		}
		logLine("починено %d через %s %s (%s)", len(done), phys.Device, gw, strings.Join(parts, ", "))
	}
	st.AddRepairs(done)
	return done
}

func uniqStrings(list []string) []string {
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

var routeWatchCmd = &cobra.Command{
	Use:   "watch [--every 10s] [--once]",
	Short: "Страж: держать маршруты обхода живыми при смене сети и переподключении VPN",
	Long: `Каждый цикл перечитывает цели из конфига и чинит маршрут, если его нет, если он ушёл в туннель
или смотрит на шлюз прошлой сети. Обычно работает службой: sudo tim route install.
Без root каждая починка спросит пароль sudo, поэтому вручную удобен --once.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := stateDir()
		if os.Geteuid() == 0 {
			dir = guardDir()
		}
		st, _ := routewatch.Load(dir)
		st.PID, st.Started = os.Getpid(), time.Now()
		if watchRouteOnce {
			done := guardCycle(&st)
			if !runner.DryRun {
				_ = st.Save(dir)
			}
			if st.Error != "" {
				return ui.Errorf("%s", st.Error)
			}
			ui.Println(ui.Dim(fmt.Sprintf("в порядке %d из %d, починено %d", st.OK, st.Want, len(done))))
			return nil
		}
		logLine("страж запущен: проверка раз в %s, состояние в %s", watchRouteEvery, routewatch.Path(dir))
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		ticker := time.NewTicker(watchRouteEvery)
		defer ticker.Stop()
		lastErr := ""
		for {
			guardCycle(&st)
			if st.Error != "" && st.Error != lastErr {
				logLine("%s", st.Error)
			}
			lastErr = st.Error
			if err := st.Save(dir); err != nil {
				logLine("состояние не записано: %v", err)
			}
			select {
			case <-stop:
				logLine("страж остановлен")
				return nil
			case <-ticker.C:
			}
		}
	},
}

func init() {
	routeWatchCmd.Flags().DurationVar(&watchRouteEvery, "every", 10*time.Second, "как часто проверять")
	routeWatchCmd.Flags().BoolVar(&watchRouteOnce, "once", false, "одна проверка и выход")
	routeCmd.AddCommand(routeWatchCmd)
}
