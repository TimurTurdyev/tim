// Package runner: запуск внешних команд с режимом dry-run.
package runner

import (
	"bytes"
	"os"
	"os/exec"
	"strings"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// DryRun печатает команды, меняющие систему, вместо запуска.
var DryRun = os.Getenv("TIM_DRY_RUN") != ""

func quote(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\"'$") {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}

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
	ui.Println(ui.Dim("$ " + quote(append([]string{name}, args...))))
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
		ui.Println(ui.Dim("$ " + quote(append([]string{name}, args...))))
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
		ui.Println(ui.Dim("$ " + strings.Join(masked, " ") + " " + quote(append([]string{name}, args...))))
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
		return false
	}
	return answer == "y" || answer == "Y" || answer == "д" || answer == "Д"
}
