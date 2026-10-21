package machine

import (
	"fmt"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/stats"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const statsLabel = "dev.tim.stats"

type statsOptions struct{ record, install, uninstall, month bool }

func runStats(o statsOptions) error {
	switch {
	case o.install:
		return installStats()
	case o.uninstall:
		if err := schedule.Uninstall(statsLabel, "tim-stats", false); err != nil {
			return err
		}
		ui.Println(ui.Label("ежечасный замер убран") + ui.Dim("  журналы остались в "+stats.Dir()))
		return nil
	case o.record:
		return stats.Append(stats.Path("local"), localSample())
	}
	days := 7
	if o.month {
		days = 30
	}
	from := time.Now().AddDate(0, 0, -days)
	printStats("эта машина", stats.Since(stats.Load(stats.Path("local")), from))
	return nil
}

func installStats() error {
	self, err := schedule.SelfPath()
	if err != nil {
		return err
	}
	job := schedule.Job{Label: statsLabel, Args: []string{self, "stats", "--record"}, Every: 3600,
		Env: schedule.JobEnv(), Log: stats.Dir() + "/record.log"}
	if err := schedule.Install(job, "tim-stats"); err != nil {
		return err
	}
	ui.Println(ui.Label("замер каждый час") + ui.Dim("  журнал в "+stats.Dir()))
	return nil
}

// localSample: замер этой машины теми же источниками, что и status.
func localSample() stats.Sample {
	s := stats.Sample{At: time.Now()}
	par.All(
		func() {
			if d, ok := sysinfo.DataDisk(); ok {
				s.DiskTotal, s.DiskFree = int64(d.Size), int64(d.Avail)
			}
		},
		func() {
			used, total := sysinfo.Memory()
			s.MemUsed, s.MemTotal = int64(used), int64(total)
		},
		func() {
			if l := sysinfo.Load(); len(l) > 0 {
				s.Load = l[0]
			}
		},
		func() {
			if b, ok := ReadBattery(); ok {
				s.Battery = b.Charge
			}
		},
	)
	return s
}

// printStats: графики за период и прогноз по одному журналу.
func printStats(name string, list []stats.Sample) {
	if len(list) == 0 {
		ui.Println(ui.Label(name) + ui.Dim("  замеров нет: tim stats --record или --install"))
		return
	}
	first, last := list[0], list[len(list)-1]
	ui.Println(ui.Label(name) + ui.Dim(fmt.Sprintf("  замеров: %d, с %s", len(list), first.At.Format("02.01 15:04"))))
	series := func(f func(stats.Sample) float64) string {
		v := make([]float64, len(list))
		for i, s := range list {
			v[i] = f(s)
		}
		return stats.Sparkline(v, 48)
	}
	row := func(label string) string { return "  " + ui.Pad(label, 10) }
	if last.DiskTotal > 0 {
		ui.Println(row("свободно") + series(func(s stats.Sample) float64 { return float64(s.DiskFree) }) +
			"  " + ui.Bytes(last.DiskFree) + ui.Dim(" из "+ui.Bytes(last.DiskTotal)))
		ui.Println(row("") + forecastLine(list))
	}
	if last.MemTotal > 0 {
		ui.Println(row("память") + series(func(s stats.Sample) float64 { return float64(s.MemUsed) }) +
			"  " + ui.Bytes(last.MemUsed) + ui.Dim(" из "+ui.Bytes(last.MemTotal)))
	}
	if last.Load > 0 {
		ui.Println(row("нагрузка") + series(func(s stats.Sample) float64 { return s.Load }) +
			"  " + strconv.FormatFloat(last.Load, 'f', 2, 64))
	}
	if last.Battery > 0 {
		ui.Println(row("батарея") + series(func(s stats.Sample) float64 { return float64(s.Battery) }) +
			"  " + strconv.Itoa(last.Battery) + "%")
	}
}

// forecastLine: прогноз свободного места, а пока замеров мало - как их завести.
func forecastLine(list []stats.Sample) string {
	text, lvl, ok := stats.ForecastLine(list)
	if !ok {
		if !schedule.Installed(statsLabel, "tim-stats", false) {
			return ui.Dim("для прогноза нужны замеры хотя бы за сутки, а ежечасный замер не стоит: tim stats --install")
		}
		return ui.Dim("для прогноза нужны замеры хотя бы за сутки")
	}
	return level.Paint(lvl, text)
}
