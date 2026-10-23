// Package motd: сводка при входе в терминал - машина, проект текущей папки, сеть, запущенное,
// сервисы, репозитории, процессы, версии.
package motd

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
)

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	var all, full, timing bool
	c := &cobra.Command{
		Use:       "status [раздел...] [--full] [--all] [--time]",
		Aliases:   []string{"motd"},
		Short:     "Сводка: машина, проект, сеть, порты, сервисы, репозитории, процессы, версии",
		Long:      "Разделы: system, project, net, ports, services, repos, procs, versions. По умолчанию ключ motd в конфиге или system net ports; --full добавляет службы.",
		ValidArgs: names(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return show(env.Cfg, env.Build(), args, all, full, timing)
		},
	}
	c.Flags().BoolVarP(&all, "all", "a", false, "все разделы")
	c.Flags().BoolVar(&full, "full", false, "со службами и пустой строкой под лок-апом")
	c.Flags().BoolVar(&timing, "time", false, "сколько занял каждый раздел")
	return []*cobra.Command{c}
}
