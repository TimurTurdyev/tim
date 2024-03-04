package cmd

import (
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/brew"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

func needBrew() error {
	if !runner.Has("brew") {
		return ui.Errorf("Homebrew не найден: https://brew.sh")
	}
	return nil
}

func serviceNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	list, _ := brew.Services()
	var names []string
	for _, s := range list {
		names = append(names, s.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
}

var svcCmd = &cobra.Command{
	Use:   "svc [start|stop|restart|logs <имя>]",
	Short: "Сервисы Homebrew: состояние, запуск, остановка, журнал",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
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
		Short:             "brew services " + action,
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: serviceNames,
		RunE: func(cmd *cobra.Command, args []string) error {
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
