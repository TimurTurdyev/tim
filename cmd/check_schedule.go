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
	self, err := os.Executable()
	if err != nil {
		return schedule.Job{}, err
	}
	if self, err = filepath.EvalSymlinks(self); err != nil {
		return schedule.Job{}, err
	}
	h, m, err := schedule.ParseAt(checkAt)
	if err != nil {
		return schedule.Job{}, ui.Errorf("%v", err)
	}
	// launchd и systemd запускают с пустым PATH: без своего git и brew обход ничего не увидит.
	env := map[string]string{"PATH": os.Getenv("PATH"), "TIM_NO_INPUT": "1"}
	if c := os.Getenv("TIM_CONFIG"); c != "" {
		env["TIM_CONFIG"] = c
	}
	return schedule.Job{Label: checkLabel, Args: []string{self, "check", "--notify"},
		Hour: h, Minute: m, Log: filepath.Join(filepath.Dir(run.StateDir()), "check.log"), Env: env}, nil
}

func launchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", checkLabel+".plist")
}

func systemdDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user")
}

func installCheck() error {
	job, err := checkJob()
	if err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		path := launchAgentPath()
		domain := "gui/" + strconv.Itoa(os.Getuid())
		_ = runner.Run("launchctl", "bootout", domain+"/"+checkLabel)
		if err := runner.WriteRoot(path, []byte(job.Plist())); err != nil {
			return err
		}
		if err := runner.Run("launchctl", "bootstrap", domain, path); err != nil {
			return err
		}
		ui.Println(ui.Label("обход каждый день в "+checkAt) + ui.Dim("  "+path))
	case "linux":
		svc, timer := job.Systemd()
		dir := systemdDir()
		if err := runner.WriteRoot(filepath.Join(dir, "tim-check.service"), []byte(svc)); err != nil {
			return err
		}
		if err := runner.WriteRoot(filepath.Join(dir, "tim-check.timer"), []byte(timer)); err != nil {
			return err
		}
		if err := runner.Run("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		if err := runner.Run("systemctl", "--user", "enable", "--now", "tim-check.timer"); err != nil {
			return err
		}
		ui.Println(ui.Label("обход каждый день в " + checkAt))
	default:
		return ui.Errorf("расписание на %s не поддержано", runtime.GOOS)
	}
	ui.Println(ui.Dim("уведомление придёт, только если что-то плохо; журнал: " + job.Log))
	return nil
}

func uninstallCheck() error {
	switch runtime.GOOS {
	case "darwin":
		_ = runner.Run("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+checkLabel)
		if err := runner.RemoveRoot(launchAgentPath()); err != nil {
			return err
		}
	case "linux":
		_ = runner.Run("systemctl", "--user", "disable", "--now", "tim-check.timer")
		for _, f := range []string{"tim-check.service", "tim-check.timer"} {
			if err := runner.RemoveRoot(filepath.Join(systemdDir(), f)); err != nil {
				return err
			}
		}
	}
	ui.Println(ui.Label("ежедневный обход убран"))
	return nil
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
