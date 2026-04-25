package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// asRoot: команда нужна под root. Забыли sudo - спрашиваем и повторяем ту же команду через sudo,
// передав путь к своему конфигу: у root своя домашняя папка. Без ввода - ошибка с готовой командой.
func asRoot() error {
	if os.Geteuid() == 0 {
		return nil
	}
	self, err := selfPath()
	if err != nil {
		return err
	}
	line := "sudo " + shellq.Join(append([]string{self}, os.Args[1:]...))
	if runner.DryRun {
		ui.Println(ui.Dim("$ " + line))
		return errDone
	}
	if runner.NoInput || !stdinTerminal() {
		return ui.Errorf("нужны права root: %s", line)
	}
	if !runner.Confirm("нужны права root - повторить через sudo?") {
		return ui.Errorf("отменено; вручную: %s", line)
	}
	return runner.Replace("sudo", append([]string{"env", "TIM_CONFIG=" + config.Path(), self}, os.Args[1:]...)...)
}

func stdinTerminal() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// errDone: команда уже всё сказала, выходим без ошибки.
var errDone = errDoneT{}

type errDoneT struct{}

func (errDoneT) Error() string { return "" }

func routesJob() (schedule.Job, error) {
	self, err := selfPath()
	if err != nil {
		return schedule.Job{}, err
	}
	env := map[string]string{"TIM_CONFIG": config.Path(), "TIM_NO_INPUT": "1",
		"PATH": "/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/bin:/opt/homebrew/bin"}
	return schedule.Job{Label: bypass.GuardLabel, Args: []string{self, "route", "watch"}, Daemon: true, Env: env, Log: bypass.GuardLog()}, nil
}

var routeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Поставить стража маршрутов службой системы: обход VPN сам чинится после смены сети",
	Long: `Ставит tim route watch постоянной службой: на macOS - LaunchDaemon dev.tim.routes, на Linux -
systemd-служба tim-routes. Нужны права root; если забыл sudo, tim спросит и повторит команду сам.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := asRoot(); err != nil {
			if err == errDone {
				return nil
			}
			return err
		}
		job, err := routesJob()
		if err != nil {
			return err
		}
		if _, err := os.Stat(config.Path()); err != nil {
			return ui.Errorf("конфиг %s не найден: страж не узнает целей", config.Path())
		}
		switch runtime.GOOS {
		case "darwin":
			_ = runner.Run("launchctl", "bootout", "system/"+bypass.GuardLabel)
			if err := runner.WriteRoot(bypass.GuardPlist(), []byte(job.Plist())); err != nil {
				return err
			}
			if err := runner.Run("launchctl", "bootstrap", "system", bypass.GuardPlist()); err != nil {
				return err
			}
		case "linux":
			svc, _ := job.Systemd()
			if err := runner.WriteRoot(bypass.GuardService(), []byte(svc)); err != nil {
				return err
			}
			if err := runner.Run("systemctl", "daemon-reload"); err != nil {
				return err
			}
			if err := runner.Run("systemctl", "enable", "--now", bypass.GuardUnit); err != nil {
				return err
			}
		default:
			return ui.Errorf("служба на %s не поддержана", runtime.GOOS)
		}
		ui.Println(ui.Label("страж установлен") + ui.Dim("  конфиг "+config.Path()+", журнал "+bypass.GuardLog()))
		if runner.DryRun {
			return nil
		}
		// Первый цикл стража идёт сразу после запуска - ждём его отчёт, чтобы сказать, как дела.
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			if st, ok := routewatch.Load(bypass.StateDir()); ok && time.Since(st.LastCheck) < 10*time.Second {
				ui.Println(ui.Dim("первая проверка: в порядке " + strconv.Itoa(st.OK) + " из " + strconv.Itoa(st.Want)))
				return nil
			}
		}
		ui.Println(ui.Warn("страж пока не отчитался; посмотри журнал: tail " + bypass.GuardLog()))
		return nil
	},
}

var routeUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Убрать стража маршрутов; сами маршруты и цели остаются",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := asRoot(); err != nil {
			if err == errDone {
				return nil
			}
			return err
		}
		switch runtime.GOOS {
		case "darwin":
			_ = runner.Run("launchctl", "bootout", "system/"+bypass.GuardLabel)
			if err := runner.RemoveRoot(bypass.GuardPlist()); err != nil {
				return err
			}
		case "linux":
			_ = runner.Run("systemctl", "disable", "--now", bypass.GuardUnit)
			if err := runner.RemoveRoot(bypass.GuardService()); err != nil {
				return err
			}
			_ = runner.Run("systemctl", "daemon-reload")
		}
		_ = os.Remove(filepath.Join(bypass.StateDir(), "routes-status.json"))
		ui.Println(ui.Label("страж убран") + ui.Dim("  после смены сети маршруты придётся поднимать: tim vpn fix"))
		return nil
	},
}

func init() {
	routeCmd.AddCommand(routeInstallCmd, routeUninstallCmd)
}
