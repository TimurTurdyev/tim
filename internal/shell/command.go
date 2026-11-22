// Package shell: tim в самой оболочке - подключение, проверка окружения, обновление.
package shell

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
)

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	return []*cobra.Command{initCommand(env), doctorCommand(env), selfUpdateCommand(env), configCommand(env), dataCommand(env)}
}

func dataCommand(env *app.Env) *cobra.Command {
	var o dataOptions
	c := app.WithJSON(&cobra.Command{
		Use:   "data [update [--nets] [--names] [--from папка]]",
		Short: "Локальная база: сети, страны и архив имён - что стоит и какого возраста",
		Long: `Локальная база заменяет сторонние сервисы в tim domain и tim ip: чья сеть и где стоит сервер - по таблицам,
поддомены - из архива имён. Лежит в ~/.local/share/tim (или TIM_DATA_DIR).
Таблицы сетей (25 МБ): маршруты из RIPE RIS, имена AS и страны серверов из ip-location-db. Качаются и без VPN.
Таблицы старше 30 дней tim не берёт и спрашивает сеть.
Архив имён (скачать около 1.9 ГБ, на диске 1 ГБ): список хостов Common Crawl, 243 млн имён, выходит раз в месяц.
Без VPN не качается: нужен VPN, зеркало ([data] mirror в конфиге) или уже скачанные части: --from папка.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return dataStatus() },
	})
	update := &cobra.Command{
		Use:   "update [--nets] [--names] [--from папка]",
		Short: "Скачать или обновить: без флагов - таблицы сетей и архив имён, если он уже стоит",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return dataUpdate(env.Cfg, o) },
	}
	update.Flags().BoolVar(&o.nets, "nets", false, "только таблицы сетей и стран")
	update.Flags().BoolVar(&o.names, "names", false, "архив имён: свежий выпуск списка хостов")
	update.Flags().StringVar(&o.from, "from", "", "собрать архив имён из папки с уже скачанными частями part-*.gz")
	c.AddCommand(update)
	return c
}

func configCommand(env *app.Env) *cobra.Command {
	var raw bool
	c := &cobra.Command{
		Use:   "config",
		Short: "Где лежит конфиг и что в нём",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return printConfig(env.Cfg, raw) },
	}
	c.Flags().BoolVar(&raw, "raw", false, "весь файл целиком")
	return app.WithJSON(c)
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
		Long:      "Добавляет функцию tim (cd, когда в экране выбрали папку), completion, алиасы и сводку из секции [shell]\nконфига.",
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
		Short: "Проверка окружения: конфиг, шим, PATH, менеджеры версий, клиенты баз, сервисы",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return diagnose(env, all) },
	}
	c.Flags().BoolVarP(&all, "all", "a", false, "и разделы без бед целиком")
	return app.WithJSON(c)
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
