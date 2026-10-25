// Package shell: tim в самой оболочке - подключение, проверка окружения, обновление.
package shell

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
)

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	return []*cobra.Command{initCommand(env), doctorCommand(env), selfUpdateCommand(env)}
}

func initCommand(env *app.Env) *cobra.Command {
	setup := &cobra.Command{
		Use:   "setup [zsh|bash]",
		Short: "Подключить tim к оболочке: дописать строку в её файл настроек",
		Args:  cobra.MaximumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return setupShell(app.First(args)) },
	}
	c := &cobra.Command{
		Use:       "init [zsh|bash]",
		Short:     "Подключить tim к оболочке; с zsh или bash печатает код для eval",
		Long:      "Добавляет функцию tim (cd для tim go, PATH для tim php use), completion, подсказку о проекте при входе\nв его папку, алиасы и сводку из секции [shell] конфига.",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"zsh", "bash"},
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return setupShell("")
			}
			return printScript(env, args[0])
		},
	}
	c.AddCommand(setup)
	return c
}

func doctorCommand(env *app.Env) *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "doctor [--all]",
		Short: "Проверка окружения: конфиг, реестр, шим, PATH, менеджеры версий, клиенты баз, сервисы",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return diagnose(env, all) },
	}
	c.Flags().BoolVarP(&all, "all", "a", false, "и разделы без бед целиком")
	return c
}

func selfUpdateCommand(env *app.Env) *cobra.Command {
	var check bool
	c := &cobra.Command{
		Use:   "self-update [--check]",
		Short: "Обновить tim до последнего релиза на GitHub",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return selfUpdate(env.Version, check) },
	}
	c.Flags().BoolVar(&check, "check", false, "только проверить, есть ли новая версия")
	return c
}
