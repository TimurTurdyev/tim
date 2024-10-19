// Package runner: запуск внешних команд с режимом dry-run.
package runner

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// DryRun печатает команды, меняющие систему, вместо запуска.
var DryRun = os.Getenv("TIM_DRY_RUN") != ""

// Output запускает команду только для чтения и возвращает stdout.
func Output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = nil
	err := cmd.Run()
	return out.String(), err
}

// Run выполняет команду, меняющую систему: в dry-run только печатает.
func Run(name string, args ...string) error {
	ui.Println(ui.Dim("$ " + shellq.Join(append([]string{name}, args...))))
	if DryRun {
		return nil
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Exec передаёт терминал команде целиком (ssh, mysql, psql).
func Exec(name string, args ...string) error {
	if DryRun {
		ui.Println(ui.Dim("$ " + shellq.Join(append([]string{name}, args...))))
		return nil
	}
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// ExecEnv как Exec, но с дополнительными переменными окружения (пароли баз).
func ExecEnv(env []string, name string, args ...string) error {
	if DryRun {
		masked := make([]string, len(env))
		for i, e := range env {
			k, _, _ := strings.Cut(e, "=")
			masked[i] = k + "=***"
		}
		ui.Println(ui.Dim("$ " + strings.Join(masked, " ") + " " + shellq.Join(append([]string{name}, args...))))
		return nil
	}
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func Sudo(args ...string) error { return Run("sudo", args...) }

// ConfirmFunc задаёт интерактивный вопрос; asked=false - спросить не вышло, спрашиваем строкой.
var ConfirmFunc func(question string) (yes, asked bool)

// Confirm спрашивает подтверждение; в dry-run всегда да.
func Confirm(question string) bool {
	if DryRun {
		return true
	}
	if ConfirmFunc != nil {
		if yes, asked := ConfirmFunc(question); asked {
			return yes
		}
	}
	ui.Printf("%s [y/N] ", question)
	var answer string
	if _, err := fmtScan(&answer); err != nil {
		ui.Println()
		return false
	}
	return answer == "y" || answer == "Y" || answer == "д" || answer == "Д"
}

// lookIn ищет программу по PATH из env, если он там задан: exec.Command смотрит только PATH самого tim.
func lookIn(env []string, name string) string {
	if strings.Contains(name, "/") {
		return name
	}
	for _, e := range env {
		path, ok := strings.CutPrefix(e, "PATH=")
		if !ok {
			continue
		}
		for _, d := range filepath.SplitList(path) {
			p := filepath.Join(d, name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
				return p
			}
		}
	}
	return name
}

// Has: программа есть в PATH.
func Has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// ExecIn как Exec, но в папке dir и с дополнительным окружением.
func ExecIn(dir string, env []string, name string, args ...string) error {
	if DryRun {
		ui.Println(ui.Dim("$ cd " + shellq.Join([]string{dir}) + " && " + strings.TrimSpace(strings.Join(env, " ")+" "+shellq.Join(append([]string{name}, args...)))))
		return nil
	}
	cmd := exec.Command(lookIn(env, name), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// CaptureIn выполняет команду, меняющую систему, в папке dir и возвращает её вывод; в dry-run только печатает.
func CaptureIn(dir, name string, args ...string) (string, error) {
	if DryRun {
		ui.Println(ui.Dim("$ cd " + shellq.Join([]string{dir}) + " && " + shellq.Join(append([]string{name}, args...))))
		return "", nil
	}
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Start запускает команду в фоне в своей группе процессов, вывод пишет в logPath; возвращает pid.
// В dry-run только печатает и возвращает 0.
func Start(dir string, env []string, logPath, name string, args ...string) (int, error) {
	if DryRun {
		ui.Println(ui.Dim("$ cd " + shellq.Join([]string{dir}) + " && " + strings.TrimSpace(strings.Join(env, " ")+" "+shellq.Join(append([]string{name}, args...))) + " > " + logPath + " &"))
		return 0, nil
	}
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, err
	}
	defer func() { _ = log.Close() }()
	cmd := exec.Command(lookIn(env, name), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	_ = cmd.Process.Release()
	return pid, nil
}

// StopGroup шлёт SIGTERM всей группе процессов pid (запущенной через Start).
func StopGroup(pid int) error {
	return Run("kill", "-TERM", "--", "-"+strconv.Itoa(pid))
}

// WriteRoot пишет файл, который может принадлежать системе: сначала напрямую,
// а если прав не хватило - через временный файл и sudo. Недостающие папки создаются.
func WriteRoot(path string, data []byte) error {
	if DryRun {
		ui.Println(ui.Dim("$ # записал бы " + path + ", " + strconv.Itoa(len(data)) + " байт"))
		return nil
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil && os.WriteFile(path, data, 0o644) == nil {
		return nil
	}
	tmp := filepath.Join(os.TempDir(), "tim-"+filepath.Base(path))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := Sudo("mkdir", "-p", filepath.Dir(path)); err != nil {
		return err
	}
	return Sudo("cp", tmp, path)
}

// RemoveRoot убирает файл, который может принадлежать системе.
func RemoveRoot(path string) error {
	if _, err := os.Stat(path); err != nil && !DryRun {
		return nil
	}
	if !DryRun && os.Remove(path) == nil {
		return nil
	}
	return Sudo("rm", "-f", path)
}
