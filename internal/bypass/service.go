package bypass

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// errRelaunched: команда уже перезапущена через sudo или показана в dry-run - выходим без ошибки.
var errRelaunched = errors.New("перезапущено через sudo")

// guardOnce: одна проверка и починка, состояние - как у службы.
func guardOnce(env *app.Env) error {
	dir := guardStateDir()
	st, _ := routewatch.Load(dir)
	st.PID, st.Started = os.Getpid(), time.Now()
	done := check(env, &st)
	if !runner.DryRun {
		_ = st.Save(dir)
	}
	if st.Error != "" {
		return ui.Errorf("%s", st.Error)
	}
	ui.Println(ui.Dim(fmt.Sprintf("в порядке %d из %d, починено %d", st.OK, st.Want, len(done))))
	return nil
}

// guard: страж - каждые every проверяет и чинит маршруты, пока его не остановят.
func guard(env *app.Env, every time.Duration) error {
	dir := guardStateDir()
	st, _ := routewatch.Load(dir)
	st.PID, st.Started = os.Getpid(), time.Now()
	LogLine("страж запущен: проверка раз в %s, состояние в %s", every, routewatch.Path(dir))
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	lastErr := ""
	for {
		check(env, &st)
		if st.Error != "" && st.Error != lastErr {
			LogLine("%s", st.Error)
		}
		lastErr = st.Error
		if err := st.Save(dir); err != nil {
			LogLine("состояние не записано: %v", err)
		}
		select {
		case <-stop:
			LogLine("страж остановлен")
			return nil
		case <-ticker.C:
		}
	}
}

// check: цели перечитываются из конфига каждый цикл - их могли добавить, пока страж работает.
func check(env *app.Env, st *routewatch.Status) []routewatch.Repair {
	c, err := config.Load()
	if err != nil {
		st.LastCheck = time.Now()
		st.Error = "конфиг: " + err.Error()
		return nil
	}
	env.Cfg = c
	return Cycle(st, c)
}

func guardStateDir() string {
	if os.Geteuid() == 0 {
		return GuardDir()
	}
	return StateDir()
}

func installGuard() error {
	if err := asRoot(); err != nil {
		return relaunched(err)
	}
	job, err := guardJob()
	if err != nil {
		return err
	}
	if _, err := os.Stat(config.Path()); err != nil {
		return ui.Errorf("конфиг %s не найден: страж не узнает целей", config.Path())
	}
	if err := schedule.Install(job, GuardUnit); err != nil {
		return err
	}
	ui.Println(ui.Label("страж установлен") + ui.Dim("  конфиг "+config.Path()+", журнал "+GuardLog()))
	if runner.DryRun {
		return nil
	}
	// Первый цикл стража идёт сразу после запуска - ждём его отчёт, чтобы сказать, как дела.
	for range 10 {
		time.Sleep(500 * time.Millisecond)
		if st, ok := routewatch.Load(StateDir()); ok && time.Since(st.LastCheck) < 10*time.Second {
			ui.Println(ui.Dim("первая проверка: в порядке " + strconv.Itoa(st.OK) + " из " + strconv.Itoa(st.Want)))
			return nil
		}
	}
	ui.Println(ui.Warn("страж пока не отчитался; посмотри журнал: tail " + GuardLog()))
	return nil
}

func uninstallGuard() error {
	if err := asRoot(); err != nil {
		return relaunched(err)
	}
	if err := schedule.Uninstall(GuardLabel, GuardUnit, true); err != nil {
		return err
	}
	if !runner.DryRun {
		_ = os.Remove(routewatch.Path(StateDir()))
	}
	ui.Println(ui.Label("страж убран") + ui.Dim("  после смены сети маршруты придётся поднимать: tim vpn fix"))
	return nil
}

func guardJob() (schedule.Job, error) {
	self, err := schedule.SelfPath()
	if err != nil {
		return schedule.Job{}, err
	}
	env := map[string]string{"TIM_CONFIG": config.Path(), "TIM_NO_INPUT": "1",
		"PATH": "/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/bin:/opt/homebrew/bin"}
	return schedule.Job{Label: GuardLabel, Args: []string{self, "route", "watch"}, Daemon: true, Env: env, Log: GuardLog()}, nil
}

// asRoot: команда нужна под root. Забыли sudo - спрашиваем и повторяем ту же команду через sudo,
// передав путь к своему конфигу: у root своя домашняя папка. Без ввода - ошибка с готовой командой.
func asRoot() error {
	if os.Geteuid() == 0 {
		return nil
	}
	self, err := schedule.SelfPath()
	if err != nil {
		return err
	}
	line := "sudo " + shellq.Join(append([]string{self}, os.Args[1:]...))
	if runner.DryRun {
		ui.Println(ui.Dim("$ " + line))
		return errRelaunched
	}
	if runner.NoInput || !stdinTerminal() {
		return ui.Errorf("нужны права root: %s", line)
	}
	if !runner.Confirm("нужны права root - повторить через sudo?") {
		return ui.Errorf("отменено; вручную: %s", line)
	}
	return runner.Replace("sudo", append([]string{"env", "TIM_CONFIG=" + config.Path(), self}, os.Args[1:]...)...)
}

// relaunched: перезапуск через sudo - не ошибка этой команды.
func relaunched(err error) error {
	if errors.Is(err, errRelaunched) {
		return nil
	}
	return err
}

func stdinTerminal() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
