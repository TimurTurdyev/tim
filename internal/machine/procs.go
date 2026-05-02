package machine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// withArgs дописывает процессам командные строки: без них эмулятор Android не отличить от любого qemu.
func withArgs(list []procs.Full) []procs.Full {
	out, err := runner.Output("ps", procs.PsAll("pid=,args=")...)
	if err != nil {
		return list
	}
	args := procs.ParseArgs(out)
	for i := range list {
		list[i].Args = args[list[i].PID]
	}
	return list
}

// HogLines: процессы, которые больше часа едят хотя бы целое ядро, строками для check и motd.
func HogLines(indent string) []string {
	list, err := All()
	if err != nil {
		return nil
	}
	var out []string
	for _, h := range procs.Hogs(list, 100, time.Hour) {
		line := indent + ui.Warn(fmt.Sprintf("%s ест %s", h.Name, procs.Cores(h.CPU)))
		detail := "уже " + ui.Duration(h.Uptime)
		if h.Note != "" {
			detail = h.Note + ", " + detail
		}
		out = append(out, line+ui.Dim("  "+detail+", pid "+h.PID+"  (tim kill "+h.PID+")"))
	}
	return out
}

func All() ([]procs.Full, error) {
	out, err := runner.Output("ps", procs.PsAll("pid=,ppid=,uid=,rss=,pcpu=,etime=,state=,comm=")...)
	if err != nil {
		return nil, err
	}
	list := withArgs(procs.ParseFull(out))
	if recent := procs.RecentCPU(500 * time.Millisecond); recent != nil {
		for i := range list {
			list[i].CPU = recent[list[i].PID]
		}
	}
	return list, nil
}

// RowName: имя строки процесса - описание, если процесс известный, иначе имя файла.
func RowName(p procs.Full) string {
	if n, note, ok := procs.Describe(p.Comm, p.Args); ok {
		if note != "" {
			return n + ", " + note
		}
		return n
	}
	return filepath.Base(p.Comm)
}

func StateHint(s string) string {
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

func Alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// waitGone ждёт, пока все процессы завершатся; возвращает тех, кто остался.
func waitGone(pids []int, limit time.Duration) []int {
	deadline := time.Now().Add(limit)
	for {
		var left []int
		for _, p := range pids {
			if Alive(p) {
				left = append(left, p)
			}
		}
		// Зомби на сигнал 0 отвечает, но он уже завершён: ждать его нечего.
		if len(left) > 0 {
			z := procs.Zombies(left)
			left = slices.DeleteFunc(left, func(p int) bool { return z[p] })
		}
		if len(left) == 0 || time.Now().After(deadline) || runner.DryRun {
			return left
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func SignalAll(sig string, pids []int) error {
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

// Killable: процессы приложения, которые можно трогать; системные не отдаём.
func Killable(a procs.App) ([]int, error) {
	for _, p := range a.Procs {
		if procs.Protected(p) {
			return nil, ui.Errorf("%s (pid %s) - системный процесс, без него машина или сеанс упадут; не трогаю", a.Name, p.PID)
		}
	}
	var pids []int
	for _, p := range a.Procs {
		if n, err := strconv.Atoi(p.PID); err == nil && n != os.Getpid() && n != os.Getppid() {
			pids = append(pids, n)
		}
	}
	if len(pids) == 0 {
		return nil, ui.Errorf("закрывать нечего")
	}
	return pids, nil
}

// Close без вопросов: бережно - Cmd+Q у приложения или SIGTERM, и подождать; force - SIGKILL.
// Что осталось живым, возвращается ошибкой: кто спрашивает, тот и решает, добивать ли.
func Close(a procs.App, force bool) (string, error) {
	pids, err := Killable(a)
	if err != nil {
		return "", err
	}
	if force {
		_ = SignalAll("KILL", pids)
		if left := waitGone(pids, 3*time.Second); len(left) > 0 {
			return "", ui.Errorf("не убился (pid %v): похоже, ждёт диск или сеть; поможет только перезагрузка", left)
		}
		return a.Name + " убит", nil
	}
	if a.IsApp && runtime.GOOS == "darwin" {
		// Приложение само спросит про несохранённое и закроет окна - бережнее, чем сигнал.
		_ = runner.Run("osascript", "-e", `quit app "`+strings.ReplaceAll(a.Name, `"`, `\"`)+`"`)
	} else {
		_ = SignalAll("TERM", pids)
	}
	// Приложению нужно время спросить про несохранённое, программе хватит и пяти секунд.
	wait := 5 * time.Second
	if a.IsApp {
		wait = 8 * time.Second
	}
	left := waitGone(pids, wait)
	if len(left) > 0 && a.IsApp && runtime.GOOS == "darwin" {
		_ = SignalAll("TERM", left)
		left = waitGone(left, 3*time.Second)
	}
	if len(left) > 0 {
		return "", ui.Errorf("%s не закрылся: осталось процессов %d; принудительно - X", a.Name, len(left))
	}
	return a.Name + " закрыт", nil
}

// StateNote: как StateHint, но без цвета - экран красит сам.
func StateNote(s string) string {
	switch {
	case strings.HasPrefix(s, "Z"):
		return "зомби: уже завершился, ждёт родителя"
	case strings.HasPrefix(s, "U"), strings.HasPrefix(s, "D"):
		return "ждёт диск или сеть и не прерывается"
	case strings.HasPrefix(s, "T"):
		return "остановлен"
	}
	return ""
}
