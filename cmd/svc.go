package cmd

import (
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/brew"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

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

func serviceNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var names []string
	if systemd() {
		for _, u := range systemdUnits() {
			names = append(names, u.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	}
	list, _ := brew.Services()
	for _, s := range list {
		names = append(names, s.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

var svcCmd = &cobra.Command{
	Use:   "svc [start|stop|restart|logs <имя>]",
	Short: "Сервисы: состояние, запуск, остановка, журнал (brew services на macOS, systemd на Linux)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if systemd() {
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
			switch s.Status {
			case "started":
				line = ui.Label(line + s.Status)
			case "error":
				code := ""
				if s.ExitCode != nil {
					code = " " + strconv.Itoa(*s.ExitCode)
				}
				line = ui.Bad(line + s.Status + code)
			default:
				line = ui.Dim(line + s.Status)
			}
			ui.Println(line)
		}
		return nil
	},
}

func svcAction(action string) *cobra.Command {
	return &cobra.Command{
		Use:               action + " <имя...>",
		Short:             action + " службы: brew services или systemctl",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: serviceNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			if systemd() {
				return runner.Sudo(append([]string{"systemctl", action}, args...)...)
			}
			if err := needBrew(); err != nil {
				return err
			}
			for _, n := range args {
				if err := runner.Run("brew", "services", action, n); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

var svcFollow bool

var svcLogsCmd = &cobra.Command{
	Use:               "logs <имя> [-f]",
	Short:             "Журнал сервиса",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: serviceNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		if systemd() {
			jargs := []string{"-u", args[0], "-n", "50", "--no-pager"}
			if svcFollow {
				jargs = append(jargs, "-f")
			}
			return runner.Exec("journalctl", jargs...)
		}
		if err := needBrew(); err != nil {
			return err
		}
		var files []string
		if s, err := brew.Info(args[0]); err == nil {
			for _, f := range []string{s.LogPath, s.ErrPath} {
				if f != "" {
					files = append(files, f)
				}
			}
		}
		if len(files) == 0 {
			files, _ = filepath.Glob(filepath.Join(brew.Prefix(), "var", "log", args[0]+"*.log"))
		}
		if len(files) == 0 {
			return ui.Errorf("журнал %s не нашёл: ни log_path в brew, ни var/log/%s*.log", args[0], args[0])
		}
		targs := []string{"-n", "50"}
		if svcFollow {
			targs = append(targs, "-F")
		}
		return runner.Exec("tail", append(targs, files...)...)
	},
}

func init() {
	svcLogsCmd.Flags().BoolVarP(&svcFollow, "follow", "f", false, "следить")
	svcCmd.AddCommand(svcAction("start"), svcAction("stop"), svcAction("restart"), svcLogsCmd)
	root.AddCommand(svcCmd)
}
