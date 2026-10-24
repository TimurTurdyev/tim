// Package serve: локальный запуск - dev-серверы проектов, их журналы и окружение, службы,
// версии PHP.
package serve

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/brew"
	"github.com/TimurTurdyev/tim/internal/run"
)

// options: как запускать - на каком порту, в фоне ли, по https, открыть ли браузер.
type options struct {
	port                int
	detach, https, open bool
}

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	return []*cobra.Command{serveCommand(env), envCommand(env), logsCommand(env), openCommand(env),
		svcCommand(), phpCommand(env), tlsProxyCommand()}
}

func serveCommand(env *app.Env) *cobra.Command {
	var o options
	c := &cobra.Command{
		Use:   "serve [проект|часть имени|@группа...] [-d] [--port N]",
		Short: "Запустить dev-сервер проекта по его стеку; несколько или с -d - в фоне",
		Long: `Команда выбирается по стеку, поле serve проекта её перекрывает. Для PHP-проектов подставляется
версия php под require.php из composer.json. Несколько проектов (и группы [groups] из конфига) запускаются
в фоне, занятые порты заменяются следующими свободными.
Фоновые: tim serve list, tim serve logs <имя> [-f], tim serve stop [имя|all].`,
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return serve(env, args, o) },
	}
	c.Flags().BoolVarP(&o.detach, "detach", "d", false, "в фоне")
	c.Flags().IntVar(&o.port, "port", 0, "порт (для одного проекта)")
	c.Flags().BoolVar(&o.https, "https", false, "отдать по https://имя.test через свой сертификат (после tim link)")
	c.Flags().BoolVar(&o.open, "open", false, "открыть в браузере, когда порт ответит")
	var follow bool
	logs := &cobra.Command{
		Use:               "logs <имя> [-f]",
		Short:             "Вывод фонового сервера",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: stateNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return backgroundLog(args[0], follow) },
	}
	logs.Flags().BoolVarP(&follow, "follow", "f", false, "следить")
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "Запущенные dev-серверы",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Interactive() && env.ProjectsScreen != nil {
				return env.Open(env.ProjectsScreen("", app.TabRunning))
			}
			printBackground()
			return nil
		},
	}, &cobra.Command{
		Use:               "stop [имя|all]",
		Short:             "Остановить фоновый сервер вместе с дочерними процессами",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: stateNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return stop(app.First(args)) },
	}, logs)
	return c
}

func envCommand(env *app.Env) *cobra.Command {
	var all, server bool
	c := &cobra.Command{
		Use:               "env [проект] [--all]",
		Short:             "Выжимка .env без секретов и сверка с реестром",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return printEnv(env, app.First(args), all) },
	}
	c.Flags().BoolVarP(&all, "all", "a", false, "все ключи, секреты скрыты")
	diff := &cobra.Command{
		Use:   "diff [проект] [--server]",
		Short: "Чем .env отличается от образца, а с --server - от .env на сервере",
		Long: `Без флага сравнивает .env с .env.example: каких ключей не хватает, какие лишние,
где значение пустое и где секрет остался из образца. С --server сравнивается только состав
ключей с .env на сервере проекта: значения с сервера не запрашиваются.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return diffEnv(env, app.First(args), server) },
	}
	diff.Flags().BoolVar(&server, "server", false, "сравнить состав ключей с .env на сервере проекта")
	c.AddCommand(diff)
	return c
}

func logsCommand(env *app.Env) *cobra.Command {
	var follow bool
	var lines int
	c := &cobra.Command{
		Use:               "logs [проект] [-f] [-n 50]",
		Short:             "Хвост самого свежего журнала проекта",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return projectLog(env, app.First(args), lines, follow) },
	}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "следить за новыми строками")
	c.Flags().IntVarP(&lines, "lines", "n", 50, "сколько строк")
	return c
}

func openCommand(env *app.Env) *cobra.Command {
	return &cobra.Command{
		Use:               "open [проект]",
		Short:             "Открыть проект в редакторе (ключ editor в конфиге, $VISUAL или Finder)",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return openEditor(env, app.First(args)) },
	}
}

func svcCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "svc [start|stop|restart|logs <имя>]",
		Short: "Сервисы: состояние, запуск, остановка, журнал (brew services на macOS, systemd на Linux)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return printServices() },
	}
	var follow bool
	logs := &cobra.Command{
		Use:               "logs <имя> [-f]",
		Short:             "Журнал сервиса",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: serviceNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return serviceLog(args[0], follow) },
	}
	logs.Flags().BoolVarP(&follow, "follow", "f", false, "следить")
	for _, action := range []string{"start", "stop", "restart"} {
		c.AddCommand(&cobra.Command{
			Use:               action + " <имя...>",
			Short:             action + " службы: brew services или systemctl",
			Args:              cobra.MinimumNArgs(1),
			ValidArgsFunction: serviceNames,
			RunE:              func(cmd *cobra.Command, args []string) error { return serviceAction(action, args) },
		})
	}
	c.AddCommand(logs)
	return c
}

func phpCommand(env *app.Env) *cobra.Command {
	c := &cobra.Command{
		Use:   "php",
		Short: "Версии PHP: установленные, активная, что требует проект",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return printPHP(env.Cfg) },
	}
	var printOnly bool
	use := &cobra.Command{
		Use:   "use <версия>",
		Short: "Переключить PHP в текущей оболочке (через шим tim init zsh)",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return usePHP(args[0], printOnly) },
	}
	use.Flags().BoolVar(&printOnly, "print", false, "только напечатать export (для шима)")
	c.AddCommand(&cobra.Command{
		Use:                "exec <версия> <команда...>",
		Short:              "Выполнить команду с нужной версией PHP первой в PATH",
		DisableFlagParsing: true,
		RunE:               func(cmd *cobra.Command, args []string) error { return execPHP(args) },
	}, use)
	return c
}

func tlsProxyCommand() *cobra.Command {
	var listen, to, cert, key string
	c := &cobra.Command{
		Use:    "tls-proxy --listen 127.0.0.1:8443 --to http://127.0.0.1:8000 --cert f --key f",
		Short:  "HTTPS-прокси перед dev-сервером (для tim serve --https -d)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   func(cmd *cobra.Command, args []string) error { return serveTLS(listen, to, cert, key) },
	}
	c.Flags().StringVar(&listen, "listen", "127.0.0.1:"+httpsPortBase, "где слушать")
	c.Flags().StringVar(&to, "to", "", "куда проксировать")
	c.Flags().StringVar(&cert, "cert", "", "сертификат")
	c.Flags().StringVar(&key, "key", "", "ключ")
	return c
}

func stateNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	var names []string
	for _, s := range run.ListStates() {
		names = append(names, s.Name)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
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
