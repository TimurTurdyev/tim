package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const checkLabel = "dev.tim.check"

var (
	checkInstall   bool
	checkUninstall bool
	checkNotify    bool
	checkAt        string
)

func checkJob() (schedule.Job, error) {
	self, err := selfPath()
	if err != nil {
		return schedule.Job{}, err
	}
	h, m, err := schedule.ParseAt(checkAt)
	if err != nil {
		return schedule.Job{}, ui.Errorf("%v", err)
	}
	env := jobEnv()
	return schedule.Job{Label: checkLabel, Args: []string{self, "check", "--notify"},
		Hour: h, Minute: m, Log: filepath.Join(filepath.Dir(run.StateDir()), "check.log"), Env: env}, nil
}

func launchAgentPath(label string) string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist")
}

func systemdDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user")
}

// installJob ставит агента launchd или таймер systemd --user; unit - имя без расширения для systemd.
func installJob(job schedule.Job, unit string) error {
	switch runtime.GOOS {
	case "darwin":
		path := launchAgentPath(job.Label)
		domain := "gui/" + strconv.Itoa(os.Getuid())
		_ = runner.Run("launchctl", "bootout", domain+"/"+job.Label)
		if err := runner.WriteRoot(path, []byte(job.Plist())); err != nil {
			return err
		}
		return runner.Run("launchctl", "bootstrap", domain, path)
	case "linux":
		svc, timer := job.Systemd()
		dir := systemdDir()
		if err := runner.WriteRoot(filepath.Join(dir, unit+".service"), []byte(svc)); err != nil {
			return err
		}
		if err := runner.WriteRoot(filepath.Join(dir, unit+".timer"), []byte(timer)); err != nil {
			return err
		}
		if err := runner.Run("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return runner.Run("systemctl", "--user", "enable", "--now", unit+".timer")
	}
	return ui.Errorf("расписание на %s не поддержано", runtime.GOOS)
}

func uninstallJob(label, unit string) error {
	switch runtime.GOOS {
	case "darwin":
		_ = runner.Run("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+label)
		return runner.RemoveRoot(launchAgentPath(label))
	case "linux":
		_ = runner.Run("systemctl", "--user", "disable", "--now", unit+".timer")
		for _, f := range []string{unit + ".service", unit + ".timer"} {
			if err := runner.RemoveRoot(filepath.Join(systemdDir(), f)); err != nil {
				return err
			}
		}
	}
	return nil
}

func installCheck() error {
	job, err := checkJob()
	if err != nil {
		return err
	}
	if err := installJob(job, "tim-check"); err != nil {
		return err
	}
	ui.Println(ui.Label("обход каждый день в " + checkAt))
	ui.Println(ui.Dim("уведомление придёт, только если что-то плохо; журнал: " + job.Log))
	return nil
}

func uninstallCheck() error {
	if err := uninstallJob(checkLabel, "tim-check"); err != nil {
		return err
	}
	ui.Println(ui.Label("ежедневный обход убран"))
	return nil
}

// jobEnv: launchd и systemd запускают с пустым PATH, без своего окружения tim ничего не найдёт.
func jobEnv() map[string]string {
	env := map[string]string{"PATH": os.Getenv("PATH"), "TIM_NO_INPUT": "1"}
	if c := os.Getenv("TIM_CONFIG"); c != "" {
		env["TIM_CONFIG"] = c
	}
	return env
}

func selfPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(self)
}

// notifyProblems: уведомление системы о плохих строках. Тихо, если всё хорошо:
// уведомление каждое утро "всё в порядке" быстро приучает его не читать.
func notifyProblems(rows []health.Row) {
	var lines []string
	bad := 0
	for _, r := range rows {
		if r.Level() != health.Bad {
			continue
		}
		bad++
		if len(lines) < 3 {
			detail := r.Name
			for _, c := range r.Problems() {
				if c.Level == health.Bad {
					detail += ": " + c.Detail
					break
				}
			}
			lines = append(lines, detail)
		}
	}
	if bad == 0 {
		return
	}
	title := "tim check: плохо в " + strconv.Itoa(bad)
	body := strings.Join(lines, "\n")
	switch runtime.GOOS {
	case "darwin":
		q := func(s string) string {
			return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
		}
		_ = runner.Run("osascript", "-e", "display notification "+q(body)+" with title "+q(title))
	case "linux":
		if runner.Has("notify-send") {
			_ = runner.Run("notify-send", title, body)
		}
	}
}
