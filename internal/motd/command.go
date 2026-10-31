// Package motd: сводка при входе в терминал - машина и текущая папка, сеть, запущенное, сервисы,
// процессы, версии.
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
		Short:     "Сводка: машина, сеть, порты, сервисы, процессы, версии",
		Long:      "Разделы: system, net, ports, services, procs, versions. По умолчанию ключ motd в конфиге или system net ports; --full добавляет службы.",
		ValidArgs: names(),
		RunE: func(cmd *cobra.Command, args []string) error {
			return show(env.Cfg, env.Build(), args, all, full, timing)
		},
	}
	c.Flags().BoolVarP(&all, "all", "a", false, "все разделы")
	c.Flags().BoolVar(&full, "full", false, "со службами")
	c.Flags().BoolVar(&timing, "time", false, "сколько занял каждый раздел")
	return []*cobra.Command{c}
}
