package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/routewatch"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/vpnhelp"
)

const (
	routesLabel = "dev.tim.routes"
	routesUnit  = "tim-routes"
)

func routesPlist() string   { return "/Library/LaunchDaemons/" + routesLabel + ".plist" }
func routesService() string { return "/etc/systemd/system/" + routesUnit + ".service" }
func routesLog() string     { return "/var/log/tim-routes.log" }

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
	line := "sudo " + self + " " + strings.Join(os.Args[1:], " ")
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
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return err
	}
	argv := append([]string{"sudo", "env", "TIM_CONFIG=" + config.Path(), self}, os.Args[1:]...)
	return syscall.Exec(sudo, argv, os.Environ())
}

func stdinTerminal() bool {
	st, err := os.Stdin.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// errDone: команда уже всё сказала, выходим без ошибки.
var errDone = errDoneT{}

type errDoneT struct{}

func (errDoneT) Error() string { return "" }

func oldDaemonPresent() bool {
	_, err := os.Stat(vpnhelp.Plist)
	return runtime.GOOS == "darwin" && err == nil
}

func routesJob() (schedule.Job, error) {
	self, err := selfPath()
	if err != nil {
		return schedule.Job{}, err
	}
	env := map[string]string{"TIM_CONFIG": config.Path(), "TIM_NO_INPUT": "1",
		"PATH": "/usr/sbin:/usr/bin:/sbin:/bin:/usr/local/bin:/opt/homebrew/bin"}
	return schedule.Job{Label: routesLabel, Args: []string{self, "route", "watch"}, Daemon: true, Env: env, Log: routesLog()}, nil
}

var routeInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Поставить стража маршрутов службой системы: обход VPN сам чинится после смены сети",
	Long: `Ставит tim route watch постоянной службой: на macOS - LaunchDaemon dev.tim.routes, на Linux -
systemd-служба tim-routes. Нужны права root; если забыл sudo, tim спросит и повторит команду сам.
Если стоит старый демон vpn-help, предложит перенести его сети в tim и снять его: два стража,
переставляющие одни маршруты, будут спорить.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Перенос - до sudo: конфиг пользователя правится от его имени.
		if os.Geteuid() != 0 && oldDaemonPresent() && !runner.NoInput {
			if runner.Confirm("стоит демон vpn-help: перенести его сети в tim как сети владельцев (as:N)?") {
				if err := routeImport(true); err != nil {
					return err
				}
			}
		}
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
			_ = runner.Run("launchctl", "bootout", "system/"+routesLabel)
			if err := runner.WriteRoot(routesPlist(), []byte(job.Plist())); err != nil {
				return err
			}
			if err := runner.Run("launchctl", "bootstrap", "system", routesPlist()); err != nil {
				return err
			}
		case "linux":
			svc, _ := job.Systemd()
			if err := runner.WriteRoot(routesService(), []byte(svc)); err != nil {
				return err
			}
			if err := runner.Run("systemctl", "daemon-reload"); err != nil {
				return err
			}
			if err := runner.Run("systemctl", "enable", "--now", routesUnit); err != nil {
				return err
			}
		default:
			return ui.Errorf("служба на %s не поддержана", runtime.GOOS)
		}
		ui.Println(ui.Label("страж установлен") + ui.Dim("  конфиг "+config.Path()+", журнал "+routesLog()))
		if oldDaemonPresent() {
			hasTargets := false
			if c, err := config.Load(); err == nil && len(c.Hosts) > 0 {
				hasTargets = true
			}
			switch {
			case !hasTargets:
				ui.Println(ui.Warn("демон vpn-help оставлен: в tim пока нет целей, без него обход пропадёт; перенеси: tim route import --by-owner"))
			case runner.Confirm("снять старый демон vpn-help? его сети теперь держит tim"):
				_ = runner.Run("launchctl", "bootout", "system/"+vpnhelp.Label)
				if err := runner.RemoveRoot(vpnhelp.Plist); err != nil {
					return err
				}
				ui.Println(ui.Label("демон vpn-help снят") + ui.Dim("  сам скрипт остался в "+vpnhelp.Script))
			}
		}
		if runner.DryRun {
			return nil
		}
		// Первый цикл стража идёт сразу после запуска - ждём его отчёт, чтобы сказать, как дела.
		for i := 0; i < 10; i++ {
			time.Sleep(500 * time.Millisecond)
			if st, ok := routewatch.Load(stateDir()); ok && time.Since(st.LastCheck) < 10*time.Second {
				ui.Println(ui.Dim("первая проверка: в порядке " + strconv.Itoa(st.OK) + " из " + strconv.Itoa(st.Want)))
				return nil
			}
		}
		ui.Println(ui.Warn("страж пока не отчитался; посмотри журнал: tail " + routesLog()))
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
			_ = runner.Run("launchctl", "bootout", "system/"+routesLabel)
			if err := runner.RemoveRoot(routesPlist()); err != nil {
				return err
			}
		case "linux":
			_ = runner.Run("systemctl", "disable", "--now", routesUnit)
			if err := runner.RemoveRoot(routesService()); err != nil {
				return err
			}
			_ = runner.Run("systemctl", "daemon-reload")
		}
		_ = os.Remove(filepath.Join(stateDir(), "routes-status.json"))
		ui.Println(ui.Label("страж убран") + ui.Dim("  после смены сети маршруты придётся поднимать: tim vpn fix"))
		return nil
	},
}

func init() {
	routeCmd.AddCommand(routeInstallCmd, routeUninstallCmd)
}
