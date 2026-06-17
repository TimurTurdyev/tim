// Package runner: запуск внешних команд с режимом dry-run.
package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// NoInput: ни о чём не спрашивать, на подтверждения отвечать "нет" (--no-input, TIM_NO_INPUT).
var NoInput = os.Getenv("TIM_NO_INPUT") != ""

// DryRun печатает команды, меняющие систему, вместо запуска.
var DryRun = os.Getenv("TIM_DRY_RUN") != ""

// ConfirmFunc задаёт интерактивный вопрос; asked=false - спросить не вышло, спрашиваем строкой.
var ConfirmFunc func(question string, def bool) (yes, asked bool)

// parseEnv: окружение утилит, чей вывод разбирается: числа с точкой, даты и сообщения по-английски.
// Кодировка остаётся пользовательской, иначе ps и lsof экранируют кириллицу в именах.
var parseEnv = sync.OnceValue(func() []string {
	env := make([]string, 0, len(os.Environ())+4)
	ctype := ""
	for _, e := range os.Environ() {
		k, v, _ := strings.Cut(e, "=")
		switch k {
		case "LC_ALL":
			ctype = v
		case "LC_NUMERIC", "LC_TIME", "LC_MESSAGES":
		default:
			env = append(env, e)
		}
	}
	if ctype != "" && os.Getenv("LC_CTYPE") == "" {
		env = append(env, "LC_CTYPE="+ctype)
	}
	return append(env, "LC_NUMERIC=C", "LC_TIME=C", "LC_MESSAGES=C")
})

// LookPath: полный путь программы из PATH.
func LookPath(name string) (string, error) { return exec.LookPath(name) }

// Output запускает команду только для чтения и возвращает stdout. Дольше минуты не ждёт: зависший
// lsof или system_profiler не должен вешать команду и экран.
func Output(name string, args ...string) (string, error) {
	return OutputWithin(time.Minute, name, args...)
}

// OutputWithin как Output, но со своим пределом времени; 0 - без предела (du по большому дереву).
func OutputWithin(limit time.Duration, name string, args ...string) (string, error) {
	return output(limit, nil, name, args...)
}

// OutputStdin как Output, но с данными на stdin: для скрипта, который уходит на сервер через ssh.
func OutputStdin(stdin, name string, args ...string) (string, error) {
	return output(time.Minute, strings.NewReader(stdin), name, args...)
}

func output(limit time.Duration, stdin io.Reader, name string, args ...string) (string, error) {
	ctx := context.Background()
	if limit > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, limit)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	cmd.Env = parseEnv()
	cmd.Stdin = stdin
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		err = fmt.Errorf("%s не ответил за %s", filepath.Base(name), limit)
	case err != nil && errOut.Len() > 0:
		err = fmt.Errorf("%w: %s", err, firstLine(errOut.String()))
	}
	return out.String(), err
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// Note печатает в dry-run то, что было бы сделано без внешней команды: "$ # записал бы ...".
func Note(what string) { ui.Println(ui.Dim("$ # " + what)) }

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
	return Stream(env, os.Stdin, os.Stdout, "", name, args...)
}

// Stream запускает команду со своими stdin и stdout; note дописывается к строке dry-run (например "> файл").
func Stream(env []string, in io.Reader, out io.Writer, note, name string, args ...string) error {
	if DryRun {
		line := shellq.Join(append([]string{name}, args...))
		if len(env) > 0 {
			masked := make([]string, len(env))
			for i, e := range env {
				k, _, _ := strings.Cut(e, "=")
				masked[i] = k + "=***"
			}
			line = strings.Join(masked, " ") + " " + line
		}
		if note != "" {
			line += " " + note
		}
		ui.Println(ui.Dim("$ " + line))
		return nil
	}
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, out, os.Stderr
	return cmd.Run()
}

func Sudo(args ...string) error {
	// Под root (страж маршрутов в launchd) sudo лишний: запускаем как есть.
	if os.Geteuid() == 0 && len(args) > 0 {
		return Run(args[0], args[1:]...)
	}
	return Run("sudo", args...)
}

// SudoQuiet: как Sudo, но вывод команды не печатается, а возвращается - для серий однотипных
// команд, где сотня строк "not in table" только мешает. Пароль sudo спрашивает сам через
// терминал, захват вывода ему не мешает.
func SudoQuiet(args ...string) (string, error) {
	name, rest := "sudo", args
	if os.Geteuid() == 0 && len(args) > 0 {
		name, rest = args[0], args[1:]
	}
	if DryRun {
		ui.Println(ui.Dim("$ " + shellq.Join(append([]string{name}, rest...))))
		return "", nil
	}
	cmd := exec.Command(name, rest...)
	cmd.Stdin = os.Stdin
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// Confirm спрашивает подтверждение, по умолчанию "нет"; в dry-run всегда да.
func Confirm(question string) bool { return ConfirmDefault(question, false) }

// ConfirmDefault спрашивает подтверждение с ответом по умолчанию. Вопрос и ответ остаются строкой
// на экране: интерактивный вопрос стирает себя, и без неё непонятно, что именно было отменено.
func ConfirmDefault(question string, def bool) bool {
	if DryRun {
		return true
	}
	if NoInput {
		ui.Println(ui.Dim(question + " - нет (без ввода)"))
		return false
	}
	if ConfirmFunc != nil {
		if yes, asked := ConfirmFunc(question, def); asked {
			ui.Println(ui.Dim(question + " - " + answer(yes)))
			return yes
		}
	}
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	ui.Printf("%s %s ", question, hint)
	var a string
	if _, err := fmtScan(&a); err != nil {
		ui.Println()
		return false
	}
	switch a {
	case "":
		return def
	case "y", "Y", "д", "Д":
		return true
	}
	return false
}

func answer(yes bool) string {
	if yes {
		return "да"
	}
	return "нет"
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
		Note("записал бы " + path + ", " + strconv.Itoa(len(data)) + " байт")
		return nil
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil && os.WriteFile(path, data, 0o644) == nil {
		return nil
	}
	// Имя случайное и файл новый: в общем /tmp чужая ссылка с тем же именем не подсунет sudo cp другой файл.
	tmp, err := os.CreateTemp("", "tim-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err != nil {
		return err
	}
	if err := Sudo("mkdir", "-p", filepath.Dir(path)); err != nil {
		return err
	}
	return Sudo("cp", tmp.Name(), path)
}

// WriteFile пишет файл пользователя целиком или никак: временный файл рядом и rename.
// В dry-run только печатает.
func WriteFile(path string, data []byte, mode os.FileMode) error {
	if DryRun {
		Note("записал бы " + path + ", " + strconv.Itoa(len(data)) + " байт")
		return nil
	}
	return WriteAtomic(path, data, mode)
}

// WriteAtomic пишет служебный файл tim (кэш, памятку, состояние) целиком или никак, без оглядки на
// dry-run: прерванная запись не оставляет полупустой файл.
func WriteAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), mode)
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
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

// Replace заменяет текущий процесс командой; возвращается только при ошибке.
func Replace(name string, args ...string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return err
	}
	return syscall.Exec(path, append([]string{name}, args...), os.Environ())
}

// InstallHint: как поставить программу на этой системе. linux - имя пакета или ссылка,
// если в репозиториях дистрибутивов её нет (начинается с http).
func InstallHint(brew, linux string) string {
	switch {
	case runtime.GOOS == "darwin":
		return "brew install " + brew
	case strings.HasPrefix(linux, "http"):
		return linux
	case Has("apt-get"):
		return "sudo apt install " + linux
	case Has("dnf"):
		return "sudo dnf install " + linux
	case Has("pacman"):
		return "sudo pacman -S " + linux
	}
	return "поставь " + linux + " пакетным менеджером системы"
}
