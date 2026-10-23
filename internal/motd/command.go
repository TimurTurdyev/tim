// Package motd: сводка при входе в терминал - машина, проект текущей папки, сеть, запущенное,
// сервисы, репозитории, процессы, версии.
package motd

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
)

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	var all, timing bool
	c := &cobra.Command{
		Use:       "status [раздел...] [--all] [--time]",
		Aliases:   []string{"motd"},
		Short:     "Сводка: машина, проект, сеть, порты, сервисы, репозитории, процессы, версии",
		Long:      "Разделы: system, project, net, ports, services, repos, procs, versions. По умолчанию ключ motd в конфиге или system project net ports services.",
		ValidArgs: names(),
		RunE:      func(cmd *cobra.Command, args []string) error { return show(env.Cfg, args, all, timing) },
	}
	c.Flags().BoolVarP(&all, "all", "a", false, "все разделы")
	c.Flags().BoolVar(&timing, "time", false, "сколько занял каждый раздел")
	return []*cobra.Command{c}
}
