package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	killForce bool
	killYes   bool
)

func allProcs() ([]procs.Full, error) {
	out, err := runner.Output("ps", "-Axo", "pid=,ppid=,uid=,rss=,pcpu=,etime=,state=,comm=")
	if err != nil {
		return nil, err
	}
	return withArgs(procs.ParseFull(out)), nil
}

// rowName: имя строки процесса - описание, если процесс известный, иначе имя файла.
func rowName(p procs.Full) string {
	if n, note, ok := procs.Describe(p.Comm, p.Args); ok {
		if note != "" {
			return n + ", " + note
		}
		return n
	}
	return filepath.Base(p.Comm)
}

func stateHint(s string) string {
	switch {
	case strings.HasPrefix(s, "Z"):
		return ui.Warn("зомби: уже завершился, ждёт родителя")
	case strings.HasPrefix(s, "U"), strings.HasPrefix(s, "D"):
		return ui.Warn("ждёт диск или сеть и не прерывается")
	case strings.HasPrefix(s, "T"):
		return ui.Warn("остановлен")
	}
	return ""
}

func printApp(a procs.App, limit int) {
	ui.Println(ui.Label(a.Name) + ui.Dim(fmt.Sprintf("  процессов %d, память %s, CPU %.1f%%, работает %s",
		len(a.Procs), fmtBytes(a.RSS), a.CPU, human(a.Main.Uptime))))
	sorted := append([]procs.Full(nil), a.Procs...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CPU*1e6+float64(sorted[i].RSS) > sorted[j].CPU*1e6+float64(sorted[j].RSS)
	})
	for i, p := range sorted {
		if i == limit {
			ui.Println(ui.Dim(fmt.Sprintf("  и ещё %d", len(sorted)-limit)))
			break
		}
		line := "  " + ui.Pad(p.PID, 7) + ui.Pad(fmt.Sprintf("%.1f%%", p.CPU), 8) + ui.Pad(fmtBytes(p.RSS), 10) + clip(rowName(p), 48)
		if p.PID == a.Main.PID && a.IsApp {
			line += ui.Dim("  главный")
		}
		if h := stateHint(p.State); h != "" {
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
		list, err := allProcs()
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

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// waitGone ждёт, пока все процессы завершатся; возвращает тех, кто остался.
func waitGone(pids []int, limit time.Duration) []int {
	deadline := time.Now().Add(limit)
	for {
		var left []int
		for _, p := range pids {
			if alive(p) {
				left = append(left, p)
			}
		}
		if len(left) == 0 || time.Now().After(deadline) || runner.DryRun {
			return left
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func signalAll(sig string, pids []int) error {
	args := []string{"-" + sig}
	for _, p := range pids {
		args = append(args, strconv.Itoa(p))
	}
	err := runner.Run("kill", args...)
	if err != nil && os.Geteuid() != 0 {
		// Чужие процессы, обычно root: без sudo kill отказывает.
		if runner.Confirm("часть процессов чужие - повторить через sudo?") {
			return runner.Sudo(append([]string{"kill"}, args...)...)
		}
	}
	return err
}

func confirmKill(q string) bool { return killYes || runner.Confirm(q) }

// confirmGentle: бережное закрытие ты и так попросил, словом kill, - Enter соглашается.
func confirmGentle(q string) bool { return killYes || runner.ConfirmDefault(q, true) }

// killApp: сначала как человек - Cmd+Q у приложения или SIGTERM у программы; не помогло - SIGKILL.
func killApp(a procs.App) error {
	for _, p := range a.Procs {
		if procs.Protected(p) {
			return ui.Errorf("%s (pid %s) - системный процесс, без него машина или сеанс упадут; не трогаю", a.Name, p.PID)
		}
	}
	var pids []int
	for _, p := range a.Procs {
		if n, err := strconv.Atoi(p.PID); err == nil && n != os.Getpid() && n != os.Getppid() {
			pids = append(pids, n)
		}
	}
	if len(pids) == 0 {
		return ui.Errorf("закрывать нечего")
	}
	if runner.DryRun {
		// В dry-run сигналы не уходят - ждать завершения и пугать "не закрылся" бессмысленно.
		if a.IsApp && runtime.GOOS == "darwin" && !killForce {
			_ = runner.Run("osascript", "-e", `quit app "`+a.Name+`"`)
		}
		_ = signalAll("TERM", pids)
		ui.Println(ui.Dim("не закроется за 8 с - следом SIGKILL; dry-run: ничего не отправлено"))
		return nil
	}
	if !killForce {
		if a.IsApp && runtime.GOOS == "darwin" {
			if !confirmGentle("закрыть " + a.Name + " как Cmd+Q?") {
				return ui.Errorf("отменено")
			}
			// Приложение само спросит про несохранённое и закроет окна - бережнее, чем сигнал.
			_ = runner.Run("osascript", "-e", `quit app "`+strings.ReplaceAll(a.Name, `"`, `\"`)+`"`)
		} else {
			if !confirmGentle(fmt.Sprintf("завершить %s (%d процессов)?", a.Name, len(pids))) {
				return ui.Errorf("отменено")
			}
			_ = signalAll("TERM", pids)
		}
		// Приложению нужно время спросить про несохранённое, программе хватит и пяти секунд.
		wait := 5 * time.Second
		if a.IsApp {
			wait = 8 * time.Second
		}
		left := waitGone(pids, wait)
		if len(left) == 0 {
			ui.Println(ui.Label(a.Name + " закрыт"))
			return nil
		}
		if a.IsApp && runtime.GOOS == "darwin" {
			_ = signalAll("TERM", left)
			left = waitGone(left, 3*time.Second)
			if len(left) == 0 {
				ui.Println(ui.Label(a.Name + " закрыт"))
				return nil
			}
		}
		ui.Println(ui.Warn(fmt.Sprintf("не закрылся: осталось процессов %d", len(left))))
		pids = left
	}
	if !confirmKill("убить принудительно? несохранённое пропадёт") {
		return ui.Errorf("оставлен как есть")
	}
	_ = signalAll("KILL", pids)
	if left := waitGone(pids, 3*time.Second); len(left) > 0 {
		return ui.Errorf("не убился (pid %v): похоже, ждёт диск или сеть; поможет только перезагрузка", left)
	}
	ui.Println(ui.Label(a.Name + " убит"))
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
		list, err := allProcs()
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
					items[i] = tui.Item{Title: a.Name, Hint: fmt.Sprintf("%d процессов, %s", len(a.Procs), fmtBytes(a.RSS))}
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
