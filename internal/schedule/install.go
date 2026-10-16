package schedule

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Install ставит задание. macOS: агент launchd пользователя, а Daemon - системный демон.
// Linux: таймер systemd (под root системный, иначе --user), а Daemon - системная служба.
// unit - имя юнита systemd без расширения.
func Install(job Job, unit string) error {
	switch runtime.GOOS {
	case "darwin":
		return installLaunchd(job)
	case "linux":
		return installSystemd(job, unit)
	}
	return ui.Errorf("задания на %s не поддержаны", runtime.GOOS)
}

// Uninstall убирает задание и его файлы.
func Uninstall(label, unit string, daemon bool) error {
	switch runtime.GOOS {
	case "darwin":
		_ = runner.Run("launchctl", "bootout", launchdDomain(daemon)+"/"+label)
		return runner.RemoveRoot(PlistPath(label, daemon))
	case "linux":
		files := []string{unit + ".service", unit + ".timer"}
		if daemon {
			_ = systemctl(true, "disable", "--now", unit)
			files = files[:1]
		} else {
			_ = systemctl(false, "disable", "--now", unit+".timer")
		}
		for _, f := range files {
			if err := runner.RemoveRoot(filepath.Join(systemdDir(daemon), f)); err != nil {
				return err
			}
		}
		return systemctl(daemon, "daemon-reload")
	}
	return nil
}

// Installed: задание стоит - есть его файл launchd или юнит systemd пользователя.
func Installed(label, unit string, daemon bool) bool {
	path := PlistPath(label, daemon)
	if runtime.GOOS == "linux" {
		path = filepath.Join(systemdDir(daemon), unit+".service")
	}
	_, err := os.Stat(path)
	return err == nil
}

// PlistPath: где лежит описание задания launchd.
func PlistPath(label string, daemon bool) string {
	if daemon {
		return "/Library/LaunchDaemons/" + label + ".plist"
	}
	return filepath.Join(paths.Home(), "Library", "LaunchAgents", label+".plist")
}

// SelfPath: настоящий путь к tim - его и запускает задание, а не ссылку, которая может уехать.
func SelfPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(self)
}

// JobEnv: launchd и systemd запускают с пустым PATH, без своего окружения tim ничего не найдёт.
func JobEnv() map[string]string {
	env := map[string]string{"PATH": os.Getenv("PATH"), "TIM_NO_INPUT": "1"}
	if c := os.Getenv("TIM_CONFIG"); c != "" {
		env["TIM_CONFIG"] = c
	}
	return env
}

// Notify: уведомление системы. Показать нечем (сервер без графики) - строкой в вывод,
// у задания по расписанию она уходит в его журнал.
func Notify(title, body string) {
	graphical := os.Getenv("DISPLAY") != "" || os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DBUS_SESSION_BUS_ADDRESS") != ""
	switch {
	case runtime.GOOS == "linux" && runner.Has("notify-send") && graphical:
		_ = runner.Run("notify-send", title, body)
	case runtime.GOOS != "darwin":
		ui.Println(ui.Warn(title) + "\n" + body)
	default:
		_ = runner.Run("osascript", "-e", "display notification "+appleQuote(body)+" with title "+appleQuote(title))
	}
}

func installLaunchd(job Job) error {
	path, domain := PlistPath(job.Label, job.Daemon), launchdDomain(job.Daemon)
	_ = runner.Run("launchctl", "bootout", domain+"/"+job.Label)
	if err := runner.WriteRoot(path, []byte(job.Plist())); err != nil {
		return err
	}
	return runner.Run("launchctl", "bootstrap", domain, path)
}

func installSystemd(job Job, unit string) error {
	svc, timer := job.Systemd()
	dir := systemdDir(job.Daemon)
	if job.Log != "" && !runner.DryRun {
		if err := os.MkdirAll(filepath.Dir(job.Log), 0o755); err != nil {
			return err
		}
	}
	if err := runner.WriteRoot(filepath.Join(dir, unit+".service"), []byte(svc)); err != nil {
		return err
	}
	start := unit
	if !job.Daemon {
		if err := runner.WriteRoot(filepath.Join(dir, unit+".timer"), []byte(timer)); err != nil {
			return err
		}
		start = unit + ".timer"
	}
	if err := systemctl(job.Daemon, "daemon-reload"); err != nil {
		return err
	}
	if err := systemctl(job.Daemon, "enable", "--now", start); err != nil {
		return err
	}
	if !job.Daemon {
		lingerHint()
	}
	return nil
}

func launchdDomain(daemon bool) string {
	if daemon {
		return "system"
	}
	return "gui/" + strconv.Itoa(os.Getuid())
}

// systemdDir: под root и для служб - системные юниты: на сервере юниты пользователя живут, только
// пока открыта сессия.
func systemdDir(daemon bool) string {
	if daemon || os.Geteuid() == 0 {
		return "/etc/systemd/system"
	}
	return filepath.Join(paths.Home(), ".config", "systemd", "user")
}

func systemctl(daemon bool, args ...string) error {
	if daemon || os.Geteuid() == 0 {
		return runner.Run("systemctl", args...)
	}
	return runner.Run("systemctl", append([]string{"--user"}, args...)...)
}

// lingerHint: без linger таймеры пользователя на сервере останавливаются вместе с последней ssh-сессией.
func lingerHint() {
	u, err := user.Current()
	if err != nil || os.Geteuid() == 0 {
		return
	}
	out, _ := runner.Output("loginctl", "show-user", u.Username, "-p", "Linger")
	if strings.TrimSpace(out) == "Linger=no" {
		ui.Println(ui.Warn("задания пользователя работают, только пока ты в системе") +
			ui.Dim("  чтобы и без входа: sudo loginctl enable-linger "+u.Username))
	}
}

func appleQuote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}
