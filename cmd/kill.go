package cmd

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	killForce bool
	killYes   bool
)

func printApp(a procs.App, limit int) {
	ui.Println(ui.Label(a.Name) + ui.Dim(fmt.Sprintf("  процессов %d, память %s, CPU %.1f%%, работает %s",
		len(a.Procs), ui.Bytes(a.RSS), a.CPU, ui.Duration(a.Main.Uptime))))
	sorted := append([]procs.Full(nil), a.Procs...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CPU*1e6+float64(sorted[i].RSS) > sorted[j].CPU*1e6+float64(sorted[j].RSS)
	})
	for i, p := range sorted {
		if i == limit {
			ui.Println(ui.Dim(fmt.Sprintf("  и ещё %d", len(sorted)-limit)))
			break
		}
		line := "  " + ui.Pad(p.PID, 7) + ui.Pad(fmt.Sprintf("%.1f%%", p.CPU), 8) + ui.Pad(ui.Bytes(p.RSS), 10) + ui.Clip(machine.RowName(p), 48)
		if p.PID == a.Main.PID && a.IsApp {
			line += ui.Dim("  главный")
		}
		if h := machine.StateHint(p.State); h != "" {
			line += "  " + h
		}
		ui.Println(line)
	}
}

var psCmd = &cobra.Command{
	Use:   "ps <имя>",
	Short: "Найти процессы по имени (с опечатками) и показать, как их закрыть",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if tui.Available() {
			return runScreen(procsScreen(args[0]))
		}
		list, err := machine.All()
		if err != nil {
			return err
		}
		apps := procs.Find(list, args[0])
		if len(apps) == 0 {
			return ui.Errorf("ничего похожего на %s не запущено; все: tim top", args[0])
		}
		for i, a := range apps {
			if i > 0 {
				ui.Println()
			}
			printApp(a, 8)
		}
		ui.Println()
		ui.Println(ui.Dim("закрыть: tim kill " + args[0] + "  (по номеру: tim kill <pid>)"))
		return nil
	},
}

func confirmKill(q string) bool { return killYes || runner.Confirm(q) }

// confirmGentle: бережное закрытие ты и так попросил, словом kill, - Enter соглашается.
func confirmGentle(q string) bool { return killYes || runner.ConfirmDefault(q, true) }

// killApp: сначала как человек - Cmd+Q у приложения или SIGTERM у программы; не помогло - SIGKILL.
func killApp(a procs.App) error {
	pids, err := machine.Killable(a)
	if err != nil {
		return err
	}
	if runner.DryRun {
		// В dry-run сигналы не уходят - ждать завершения и пугать "не закрылся" бессмысленно.
		if a.IsApp && runtime.GOOS == "darwin" && !killForce {
			_ = runner.Run("osascript", "-e", `quit app "`+a.Name+`"`)
		}
		_ = machine.SignalAll("TERM", pids)
		ui.Println(ui.Dim("не закроется за 8 с - следом SIGKILL; dry-run: ничего не отправлено"))
		return nil
	}
	if !killForce {
		q := fmt.Sprintf("завершить %s (%d процессов)?", a.Name, len(pids))
		if a.IsApp && runtime.GOOS == "darwin" {
			q = "закрыть " + a.Name + " как Cmd+Q?"
		}
		if !confirmGentle(q) {
			return ui.Errorf("отменено")
		}
		text, err := machine.Close(a, false)
		if err == nil {
			ui.Println(ui.Label(text))
			return nil
		}
		ui.Println(ui.Warn(strings.TrimSuffix(err.Error(), "; принудительно - X")))
	}
	if !confirmKill("убить принудительно? несохранённое пропадёт") {
		return ui.Errorf("оставлен как есть")
	}
	text, err := machine.Close(a, true)
	if err != nil {
		return err
	}
	ui.Println(ui.Label(text))
	return nil
}

var killCmd = &cobra.Command{
	Use:   "kill <имя|pid> [--force] [-y]",
	Short: "Закрыть зависшее приложение или процесс: сначала бережно, потом принудительно",
	Long: `Ищет по имени с опечатками ("goglehrome" найдёт Google Chrome) и закрывает все процессы
приложения разом. Приложение сначала закрывается как Cmd+Q - оно успеет спросить про несохранённое.
Не закрылось за несколько секунд - tim спросит, убить ли принудительно. Обычная программа сначала
получает SIGTERM, потом SIGKILL. Системные процессы (launchd, WindowServer и т.п.) не трогаются.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		list, err := machine.All()
		if err != nil {
			return err
		}
		var app procs.App
		if _, err := strconv.Atoi(args[0]); err == nil {
			for _, p := range list {
				if p.PID == args[0] {
					app = procs.App{Name: procs.Name(p.Comm, p.Args), Procs: []procs.Full{p}, Main: p, RSS: p.RSS, CPU: p.CPU,
						IsApp: strings.Contains(p.Comm, ".app/")}
				}
			}
			if app.Name == "" {
				return ui.Errorf("процесса %s нет", args[0])
			}
		} else {
			apps := procs.Find(list, args[0])
			switch {
			case len(apps) == 0:
				return ui.Errorf("ничего похожего на %s не запущено; все: tim top", args[0])
			case len(apps) == 1:
				app = apps[0]
			default:
				items := make([]tui.Item, len(apps))
				for i, a := range apps {
					items[i] = tui.Item{Title: a.Name, Hint: fmt.Sprintf("%d процессов, %s", len(a.Procs), ui.Bytes(a.RSS))}
				}
				k, err := tui.Pick("что закрыть", items)
				if err != nil {
					return pickErr(err, "точное имя", items)
				}
				app = apps[k]
			}
		}
		printApp(app, 5)
		return killApp(app)
	},
}

func init() {
	killCmd.Flags().BoolVar(&killForce, "force", false, "сразу принудительно (SIGKILL)")
	killCmd.Flags().BoolVarP(&killYes, "yes", "y", false, "не спрашивать")
	root.AddCommand(psCmd, killCmd)
}
