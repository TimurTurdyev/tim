package serve

import (
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/brew"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/phpver"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// brewStatus: состояния brew services по-русски; незнакомое показывается как есть.
var brewStatus = map[string]string{"started": "работает", "stopped": "остановлен", "none": "остановлен",
	"error": "ошибка", "scheduled": "по расписанию", "unknown": "неизвестно"}

// systemd: на Linux службы - юниты systemd, а не brew services.
func systemd() bool { return runtime.GOOS == "linux" && runner.Has("systemctl") }

func systemdUnits() []sysinfo.Unit {
	out, _ := runner.Output("systemctl", "list-units", "--type=service", "--state=running,failed", "--no-legend", "--plain")
	return sysinfo.ParseUnits(out)
}

func needBrew() error {
	if runtime.GOOS == "linux" {
		return ui.Errorf("systemd здесь нет (контейнер?): службами tim управлять не может")
	}
	if !runner.Has("brew") {
		return ui.Errorf("Homebrew не найден: https://brew.sh")
	}
	return nil
}

func printServices() error {
	if systemd() {
		printUnits()
		return nil
	}
	if err := needBrew(); err != nil {
		return err
	}
	list, err := brew.Services()
	if err != nil {
		return ui.Errorf("brew services: %v", err)
	}
	for _, s := range list {
		line := ui.Pad(s.Name, 22) + " "
		status := s.Status
		if ru, ok := brewStatus[status]; ok {
			status = ru
		}
		switch s.Status {
		case "started":
			line = ui.Label(line + status)
		case "error":
			code := ""
			if s.ExitCode != nil {
				code = ", код " + strconv.Itoa(*s.ExitCode)
			}
			line = ui.Bad(line + status + code)
		default:
			line = ui.Dim(line + status)
		}
		ui.Println(line)
	}
	return nil
}

func printUnits() {
	units := systemdUnits()
	if len(units) == 0 {
		ui.Println(ui.Dim("работающих служб не видно"))
	}
	for _, u := range units {
		line := ui.Pad(u.Name, 28) + " " + ui.Pad(u.Sub, 9)
		if u.Active == "failed" {
			ui.Println(ui.Bad(line) + ui.Dim(u.Description) + ui.Dim("  tim svc logs "+u.Name))
			continue
		}
		ui.Println(ui.Label(line) + ui.Dim(u.Description))
	}
}

func serviceAction(action string, names []string) error {
	if systemd() {
		return runner.Sudo(append([]string{"systemctl", action}, names...)...)
	}
	if err := needBrew(); err != nil {
		return err
	}
	for _, n := range names {
		if err := runner.Run("brew", "services", action, n); err != nil {
			return err
		}
	}
	return nil
}

func serviceLog(name string, follow bool) error {
	if systemd() {
		args := []string{"-u", name, "-n", "50", "--no-pager"}
		if follow {
			args = append(args, "-f")
		}
		return runner.Exec("journalctl", args...)
	}
	if err := needBrew(); err != nil {
		return err
	}
	var files []string
	if s, err := brew.Info(name); err == nil {
		for _, f := range []string{s.LogPath, s.ErrPath} {
			if f != "" {
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		files, _ = filepath.Glob(filepath.Join(brew.Prefix(), "var", "log", name+"*.log"))
	}
	if len(files) == 0 {
		return ui.Errorf("журнал %s не нашёл: ни log_path в brew, ни var/log/%s*.log", name, name)
	}
	return tail(50, follow, files...)
}

// printPHP: установленные версии, активная и что требует проект текущей папки.
func printPHP(cfg *config.Config) error {
	if runtime.GOOS == "darwin" {
		if err := needBrew(); err != nil {
			return err
		}
	}
	active, _ := runner.Output("php", "-r", "echo PHP_VERSION;")
	var versions []phpver.Version
	for _, i := range phpver.Installs() {
		mark := "  "
		if strings.HasPrefix(active, i.Version.String()+".") {
			mark = ui.Label("* ")
		}
		ui.Println(mark + ui.Pad(i.Version.String(), 6) + " " + ui.Dim(i.Bin))
		versions = append(versions, i.Version)
	}
	if active != "" {
		ui.Println(ui.Dim("активная " + active))
	}
	if p, err := cfg.Pick(""); err == nil {
		if req := phpver.Required(filepath.Join(cfg.ProjectDir(p), "composer.json")); req != "" {
			var fit []string
			for _, v := range phpver.Matching(versions, req) {
				fit = append(fit, v.String())
			}
			line := ui.Label(p.Name) + " требует php " + req
			if len(fit) == 0 {
				line += "  " + ui.Bad("подходящей версии нет")
			} else {
				line += ui.Dim("  подходят: " + strings.Join(fit, " "))
			}
			ui.Println(line)
		}
	}
	ui.Println(ui.Dim("tim php use 8.3 - в этой оболочке; tim php exec 8.3 <команда> - один раз"))
	return nil
}
