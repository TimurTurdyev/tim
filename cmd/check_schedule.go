package cmd

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/run"
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
	self, err := schedule.SelfPath()
	if err != nil {
		return schedule.Job{}, err
	}
	h, m, err := schedule.ParseAt(checkAt)
	if err != nil {
		return schedule.Job{}, ui.Errorf("%v", err)
	}
	env := schedule.JobEnv()
	return schedule.Job{Label: checkLabel, Args: []string{self, "check", "--notify"},
		Hour: h, Minute: m, Log: filepath.Join(filepath.Dir(run.StateDir()), "check.log"), Env: env}, nil
}

func installCheck() error {
	job, err := checkJob()
	if err != nil {
		return err
	}
	if err := schedule.Install(job, "tim-check"); err != nil {
		return err
	}
	ui.Println(ui.Label("обход каждый день в " + checkAt))
	ui.Println(ui.Dim("уведомление придёт, только если что-то плохо; журнал: " + job.Log))
	return nil
}

func uninstallCheck() error {
	if err := schedule.Uninstall(checkLabel, "tim-check", false); err != nil {
		return err
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
	schedule.Notify("tim check: плохо в "+strconv.Itoa(bad), strings.Join(lines, "\n"))
}
