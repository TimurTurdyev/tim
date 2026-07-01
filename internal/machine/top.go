package machine

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

type topOptions struct {
	by    string
	count int
	cpu   bool
}

type killOptions struct{ force, yes bool }

func printTop(cfg *config.Config, o topOptions) error {
	full, err := procs.All()
	if err != nil {
		return err
	}
	list := make([]procs.Sample, len(full))
	for i, p := range full {
		list[i] = p.Sample
	}
	key := func(s procs.Sample) string { return procs.Name(s.Comm, s.Args) }
	label := "приложение"
	switch o.by {
	case "app":
	case "project":
		key, label = projectOf(cfg), "проект"
	default:
		return ui.Errorf("--by app или project, а не %s", o.by)
	}
	groups := procs.GroupBy(list, key, o.cpu)
	if len(groups) == 0 {
		ui.Println(ui.Dim("процессов проектов не нашлось"))
		return nil
	}
	var total int64
	for _, g := range groups {
		total += g.RSS
	}
	ui.Println(ui.Dim(ui.Pad(label, 28) + ui.Pad("память", 10) + ui.Pad("CPU", 8) + "процессов"))
	for i, g := range groups {
		if i == o.count {
			ui.Println(ui.Dim(fmt.Sprintf("и ещё %d", len(groups)-o.count)))
			break
		}
		ui.Println(ui.Label(ui.Pad(ui.Clip(g.Name, 27), 28)) + ui.Pad(ui.Bytes(g.RSS), 10) +
			ui.Pad(strconv.FormatFloat(g.CPU, 'f', 1, 64)+"%", 8) + ui.Dim(strconv.Itoa(g.Count)))
	}
	if o.by == "project" {
		ui.Println(ui.Dim("всего у проектов " + ui.Bytes(total)))
	}
	return nil
}

// projectOf: проект по рабочей папке процесса; приложения из .app в проекты не записываем -
// они стартуют в той папке, откуда их открыли.
func projectOf(cfg *config.Config) func(procs.Sample) string {
	cwd := procs.Cwds(nil)
	return func(s procs.Sample) string {
		dir := cwd[s.PID]
		if dir == "" || dir == "/" || strings.Contains(s.Comm, ".app/") {
			return ""
		}
		if p, ok := cfg.ProjectHere(dir); ok {
			return p.Name
		}
		return ""
	}
}

func printFound(query string) error {
	list, err := procs.All()
	if err != nil {
		return err
	}
	apps := procs.Find(list, query)
	if len(apps) == 0 {
		return ui.Errorf("ничего похожего на %s не запущено; все: tim top", query)
	}
	for i, a := range apps {
		if i > 0 {
			ui.Println()
		}
		printApp(a, 8)
	}
	ui.Println()
	ui.Println(ui.Dim("закрыть: tim kill " + query + "  (по номеру: tim kill <pid>)"))
	return nil
}

func printApp(a procs.App, limit int) {
	ui.Println(ui.Label(a.Name) + ui.Dim(fmt.Sprintf("  процессов %d, память %s, CPU %.1f%%, работает %s",
		len(a.Procs), ui.Bytes(a.RSS), a.CPU, ui.Duration(a.Main.Uptime))))
	sorted := busiestFirst(a.Procs)
	for i, p := range sorted {
		if i == limit {
			ui.Println(ui.Dim(fmt.Sprintf("  и ещё %d", len(sorted)-limit)))
			break
		}
		line := "  " + ui.Pad(p.PID, 7) + ui.Pad(fmt.Sprintf("%.1f%%", p.CPU), 8) + ui.Pad(ui.Bytes(p.RSS), 10) + ui.Clip(RowName(p), 48)
		if p.PID == a.Main.PID && a.IsApp {
			line += ui.Dim("  главный")
		}
		if h := StateHint(p.State); h != "" {
			line += "  " + h
		}
		ui.Println(line)
	}
}

// busiestFirst: процессы по CPU, при равном - по памяти.
func busiestFirst(list []procs.Full) []procs.Full {
	sorted := append([]procs.Full(nil), list...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CPU != sorted[j].CPU {
			return sorted[i].CPU > sorted[j].CPU
		}
		return sorted[i].RSS > sorted[j].RSS
	})
	return sorted
}

// kill: процесс по номеру или приложение по имени с опечатками; несколько похожих - выбором.
func kill(query string, o killOptions) error {
	list, err := procs.All()
	if err != nil {
		return err
	}
	target, err := killTarget(list, query)
	if err != nil {
		return err
	}
	printApp(target, 5)
	return killApp(target, o)
}

func killTarget(list []procs.Full, query string) (procs.App, error) {
	if _, err := strconv.Atoi(query); err == nil {
		for _, p := range list {
			if p.PID == query {
				return procs.App{Name: procs.Name(p.Comm, p.Args), Procs: []procs.Full{p}, Main: p, RSS: p.RSS, CPU: p.CPU,
					IsApp: strings.Contains(p.Comm, ".app/")}, nil
			}
		}
		return procs.App{}, ui.Errorf("процесса %s нет", query)
	}
	apps := procs.Find(list, query)
	switch len(apps) {
	case 0:
		return procs.App{}, ui.Errorf("ничего похожего на %s не запущено; все: tim top", query)
	case 1:
		return apps[0], nil
	}
	opts := make([]app.Option, len(apps))
	for i, a := range apps {
		opts[i] = app.Option{Title: a.Name, Hint: fmt.Sprintf("%d процессов, %s", len(a.Procs), ui.Bytes(a.RSS))}
	}
	k, err := app.Choose("что закрыть", "точное имя", opts)
	if err != nil {
		return procs.App{}, err
	}
	return apps[k], nil
}

// killApp: сначала как человек - Cmd+Q у приложения или SIGTERM у программы; не помогло - SIGKILL.
func killApp(a procs.App, o killOptions) error {
	pids, err := Killable(a)
	if err != nil {
		return err
	}
	if runner.DryRun {
		// В dry-run сигналы не уходят - ждать завершения и пугать "не закрылся" бессмысленно.
		if a.IsApp && runtime.GOOS == "darwin" && !o.force {
			_ = runner.Run("osascript", "-e", `quit app "`+a.Name+`"`)
		}
		_ = SignalAll("TERM", pids)
		ui.Println(ui.Dim("не закроется за 8 с - следом SIGKILL; dry-run: ничего не отправлено"))
		return nil
	}
	if !o.force {
		q := fmt.Sprintf("завершить %s (%d процессов)?", a.Name, len(pids))
		if a.IsApp && runtime.GOOS == "darwin" {
			q = "закрыть " + a.Name + " как Cmd+Q?"
		}
		// Бережное закрытие ты и так попросил, словом kill, - Enter соглашается.
		if !o.yes && !runner.ConfirmDefault(q, true) {
			return ui.Errorf("отменено")
		}
		text, err := Close(a, false)
		if err == nil {
			ui.Println(ui.Label(text))
			return nil
		}
		ui.Println(ui.Warn(strings.TrimSuffix(err.Error(), "; принудительно - X")))
	}
	if !o.yes && !runner.Confirm("убить принудительно? несохранённое пропадёт") {
		return ui.Errorf("оставлен как есть")
	}
	text, err := Close(a, true)
	if err != nil {
		return err
	}
	ui.Println(ui.Label(text))
	return nil
}
