package cmd

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/stats"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const statsLabel = "dev.tim.stats"

var (
	statsRecord    bool
	statsInstall   bool
	statsUninstall bool
	statsMonth     bool
)

// localSample: замер этой машины теми же источниками, что и motd.
func localSample() stats.Sample {
	s := stats.Sample{At: time.Now()}
	var wg sync.WaitGroup
	var mu sync.Mutex
	wg.Go(func() {
		out, _ := runner.Output("df", "-k", "/System/Volumes/Data")
		if d, ok := sysinfo.ParseDF(out); ok {
			mu.Lock()
			s.DiskTotal, s.DiskFree = int64(d.Size), int64(d.Size-d.Used)
			mu.Unlock()
		}
	})
	wg.Go(func() {
		vm, _ := runner.Output("vm_stat")
		t, _ := runner.Output("sysctl", "-n", "hw.memsize")
		total, _ := strconv.ParseInt(strings.TrimSpace(t), 10, 64)
		mu.Lock()
		s.MemUsed, s.MemTotal = int64(sysinfo.ParseVMStat(vm)), total
		mu.Unlock()
	})
	wg.Go(func() {
		out, _ := runner.Output("sysctl", "-n", "vm.loadavg")
		if l := sysinfo.ParseLoadavg(out); len(l) > 0 {
			mu.Lock()
			s.Load = l[0]
			mu.Unlock()
		}
	})
	wg.Go(func() {
		if b, ok := readBattery(); ok {
			mu.Lock()
			s.Battery = b.Charge
			mu.Unlock()
		}
	})
	wg.Wait()
	return s
}

// forecastLine: человеческая строка прогноза свободного места.
func forecastLine(list []stats.Sample) string {
	perDay, left, ok := stats.Forecast(list)
	switch {
	case !ok:
		return ui.Dim("для прогноза нужны замеры хотя бы за сутки")
	case math.IsInf(left, 1) || perDay > -float64(100<<20):
		return "место не убывает" + ui.Dim(fmt.Sprintf("  %+s в сутки", signedBytes(perDay)))
	}
	line := fmt.Sprintf("уходит %s в сутки, кончится через %.0f дн", fmtBytes(int64(-perDay)), left)
	if left < 14 {
		return ui.Bad(line)
	}
	if left < 60 {
		return ui.Warn(line)
	}
	return line
}

func signedBytes(v float64) string {
	if v < 0 {
		return "-" + fmtBytes(int64(-v))
	}
	return "+" + fmtBytes(int64(v))
}

// printStats: графики за период и прогноз по одному журналу.
func printStats(name string, list []stats.Sample) {
	if len(list) == 0 {
		ui.Println(ui.Label(name) + ui.Dim("  замеров нет: tim stats --record или --install"))
		return
	}
	first, last := list[0], list[len(list)-1]
	ui.Println(ui.Label(name) + ui.Dim(fmt.Sprintf("  замеров: %d, с %s", len(list), first.At.Format("02.01 15:04"))))
	series := func(f func(stats.Sample) float64) []float64 {
		v := make([]float64, len(list))
		for i, s := range list {
			v[i] = f(s)
		}
		return v
	}
	const width = 48
	if last.DiskTotal > 0 {
		ui.Println("  " + ui.Pad("свободно", 10) + stats.Sparkline(series(func(s stats.Sample) float64 { return float64(s.DiskFree) }), width) +
			"  " + fmtBytes(last.DiskFree) + ui.Dim(" из "+fmtBytes(last.DiskTotal)))
		ui.Println("  " + ui.Pad("", 10) + forecastLine(list))
	}
	if last.MemTotal > 0 {
		ui.Println("  " + ui.Pad("память", 10) + stats.Sparkline(series(func(s stats.Sample) float64 { return float64(s.MemUsed) }), width) +
			"  " + fmtBytes(last.MemUsed) + ui.Dim(" из "+fmtBytes(last.MemTotal)))
	}
	if last.Load > 0 {
		ui.Println("  " + ui.Pad("нагрузка", 10) + stats.Sparkline(series(func(s stats.Sample) float64 { return s.Load }), width) +
			"  " + strconv.FormatFloat(last.Load, 'f', 2, 64))
	}
	if last.Battery > 0 {
		ui.Println("  " + ui.Pad("батарея", 10) + stats.Sparkline(series(func(s stats.Sample) float64 { return float64(s.Battery) }), width) +
			"  " + strconv.Itoa(last.Battery) + "%")
	}
}

var statsCmd = &cobra.Command{
	Use:   "stats [--month] [--record] [--install] [--uninstall]",
	Short: "История машины: место, память, нагрузка, батарея и когда кончится диск",
	Long: `Показывает графики за неделю (--month - за месяц) по журналу замеров и прогноз свободного места.
Замеры пишет tim stats --record; tim stats --install ставит его каждый час (launchd, на Linux - systemd).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		switch {
		case statsInstall:
			self, err := selfPath()
			if err != nil {
				return err
			}
			job := schedule.Job{Label: statsLabel, Args: []string{self, "stats", "--record"}, Every: 3600, Env: jobEnv(), Log: stats.Dir() + "/record.log"}
			if err := installJob(job, "tim-stats"); err != nil {
				return err
			}
			ui.Println(ui.Label("замер каждый час") + ui.Dim("  журнал в "+stats.Dir()))
			return nil
		case statsUninstall:
			if err := uninstallJob(statsLabel, "tim-stats"); err != nil {
				return err
			}
			ui.Println(ui.Label("ежечасный замер убран") + ui.Dim("  журналы остались в "+stats.Dir()))
			return nil
		case statsRecord:
			return stats.Append(stats.Path("local"), localSample())
		}
		days := 7
		if statsMonth {
			days = 30
		}
		from := time.Now().AddDate(0, 0, -days)
		printStats("эта машина", stats.Since(stats.Load(stats.Path("local")), from))
		return nil
	},
}

func init() {
	statsCmd.Flags().BoolVar(&statsRecord, "record", false, "сделать замер и дописать в журнал")
	statsCmd.Flags().BoolVar(&statsInstall, "install", false, "замер каждый час")
	statsCmd.Flags().BoolVar(&statsUninstall, "uninstall", false, "убрать ежечасный замер")
	statsCmd.Flags().BoolVar(&statsMonth, "month", false, "за месяц, а не за неделю")
	root.AddCommand(statsCmd)
}
