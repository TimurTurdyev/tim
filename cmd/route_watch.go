package cmd

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	watchRouteEvery time.Duration
	watchRouteOnce  bool
)

var routeWatchCmd = &cobra.Command{
	Use:   "watch [--every 10s] [--once]",
	Short: "Страж: держать маршруты обхода живыми при смене сети и переподключении VPN",
	Long: `Каждый цикл перечитывает цели из конфига и чинит маршрут, если его нет, если он ушёл в туннель
или смотрит на шлюз прошлой сети. Обычно работает службой: sudo tim route install.
Без root каждая починка спросит пароль sudo, поэтому вручную удобен --once.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := bypass.StateDir()
		if os.Geteuid() == 0 {
			dir = bypass.GuardDir()
		}
		st, _ := routewatch.Load(dir)
		st.PID, st.Started = os.Getpid(), time.Now()
		if watchRouteOnce {
			c, done := bypass.Cycle(&st, config.Load)
			if c != nil {
				cfg = c
			}
			if !runner.DryRun {
				_ = st.Save(dir)
			}
			if st.Error != "" {
				return ui.Errorf("%s", st.Error)
			}
			ui.Println(ui.Dim(fmt.Sprintf("в порядке %d из %d, починено %d", st.OK, st.Want, len(done))))
			return nil
		}
		bypass.LogLine("страж запущен: проверка раз в %s, состояние в %s", watchRouteEvery, routewatch.Path(dir))
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		ticker := time.NewTicker(watchRouteEvery)
		defer ticker.Stop()
		lastErr := ""
		for {
			if c, _ := bypass.Cycle(&st, config.Load); c != nil {
				cfg = c
			}
			if st.Error != "" && st.Error != lastErr {
				bypass.LogLine("%s", st.Error)
			}
			lastErr = st.Error
			if err := st.Save(dir); err != nil {
				bypass.LogLine("состояние не записано: %v", err)
			}
			select {
			case <-stop:
				bypass.LogLine("страж остановлен")
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
