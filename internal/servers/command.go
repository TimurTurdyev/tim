// Package servers: свои серверы - ssh мимо VPN, отчёт о состоянии, туннели, базы, приёмник вебхуков.
package servers

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// exposeOptions: куда показать локальный порт - frps, свой сервер по ssh или cloudflared.
type exposeOptions struct {
	frp, ssh string
	remote   int
}

// hookOptions: чем отвечать на вебхук и показывать ли его наружу.
type hookOptions struct {
	status       int
	body         string
	expose, list bool
}

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	return []*cobra.Command{sshCommand(env), serverCommand(env), serversCommand(env), tunnelCommand(env), dbCommand(env), hookCommand(env)}
}

func sshCommand(env *app.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "ssh [алиас|проект|user@host] [аргументы ssh]",
		Short: "ssh с обходом VPN: если адрес уходит в туннель, предложит маршрут",
		Long: `Цель может быть алиасом из ~/.ssh/config, именем проекта из конфига или user@host.
Перед подключением tim смотрит, куда уходит адрес. Если в туннель VPN или на шлюз
прежней сети, предлагает завести host-маршрут через физический шлюз и только потом
подключается. Без аргументов открывает выбор алиаса, ssh list печатает их списком.`,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveDefault
			}
			names, _ := app.ProjectNames(cmd, args, toComplete)
			return append(sshx.Aliases(), names...), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error { return ssh(env, args) },
	}
}

func serverCommand(env *app.Env) *cobra.Command {
	var asJSON, attacks, traffic bool
	var since string
	c := &cobra.Command{
		Use:   "server <алиас|проект|user@host> [--attacks | --traffic] [--since 24h] [--json]",
		Short: "Состояние Linux-сервера по ssh: место, память, юниты, обновления, порты наружу, sshd",
		Long: `Один заход по ssh, только чтение: скрипт уходит через stdin и ничего не ставит и не меняет.
То, что требует root (sshd -T, nginx -t, сертификаты), идёт через sudo -n: без пароля он не спрашивает,
а отказывает, и такие проверки помечаются "не проверено".
--attacks читает журналы: удачные входы по ssh (вход с адреса, который перебирал пароли, - красный),
перебор, пробы сайта (.env, .git, wp-login), файрвол, баны fail2ban и CrowdSec, владельцев сетей.
--traffic читает журналы запросов nginx или apache: сколько запросов и в минуту, доли ответов,
кто ходит и куда, ошибки 5xx, пути в пустоту, время ответа, если журнал его пишет.
Журналы ssh и ядра читают root и группы adm и systemd-journal; журналы сайта панели держат в папке
пользователя - их tim находит сам.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := Target(env.Cfg, args[0])
			if attacks && traffic {
				return ui.Errorf("--attacks и --traffic - два разных отчёта, по одному за раз")
			}
			if cmd.Flags().Changed("since") && !attacks && !traffic {
				return ui.Errorf("--since работает вместе с --attacks или --traffic")
			}
			switch {
			case attacks:
				return withWindow(since, 24*time.Hour, func(w time.Duration) error {
					if app.Interactive() && !asJSON {
						return env.Open(AttacksScreen(target, w))
					}
					return printAttacks(target, w, asJSON)
				})
			case traffic:
				return withWindow(since, time.Hour, func(w time.Duration) error {
					if app.Interactive() && !asJSON {
						return env.Open(TrafficScreen(target, w))
					}
					return printTraffic(target, w, asJSON)
				})
			case app.Interactive() && !asJSON:
				return env.Open(Screen(env, target))
			}
			return printServer(target, asJSON)
		},
	}
	c.Flags().BoolVar(&asJSON, "json", false, "отчёт в JSON")
	c.Flags().BoolVar(&attacks, "attacks", false, "кто ломится: перебор ssh, удачные входы, пробы сайта, баны - по журналам")
	c.Flags().BoolVar(&traffic, "traffic", false, "кто ходит: запросы, ошибки, адреса, пути и время ответа - по журналам nginx")
	c.Flags().StringVar(&since, "since", "", "окно: 24h, 3d, 1w; у --attacks по умолчанию 24h, у --traffic 1h")
	return c
}

// withWindow: окно из флага или по умолчанию для отчёта.
func withWindow(since string, def time.Duration, run func(time.Duration) error) error {
	w := def
	if since != "" {
		var err error
		if w, err = parseSince(since); err != nil {
			return err
		}
	}
	return run(w)
}

func serversCommand(env *app.Env) *cobra.Command {
	var o serversOptions
	c := &cobra.Command{
		Use:   "servers [алиас...] [--all] [--json] [--record | --install | --uninstall]",
		Short: "Все серверы одной таблицей: состояние, uptime, нагрузка, память, диск",
		Long: `Те же отчёты, что tim server, по всем серверам сразу: без аргументов - по ssh проектов из реестра,
--all - по всем алиасам ~/.ssh/config. По 6 серверов за раз, каждому 15 секунд, только чтение.
--record дописывает замер в журнал каждого сервера, по нему tim server показывает, с какой скоростью
уходит место и когда кончится. --install ставит такой замер раз в 6 часов (launchd, на Linux - systemd).`,
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return sshx.Aliases(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error { return runServers(env, args, o) },
	}
	f := c.Flags()
	f.BoolVar(&o.all, "all", false, "все алиасы ~/.ssh/config, а не только серверы проектов")
	f.BoolVar(&o.json, "json", false, "отчёты в JSON")
	f.BoolVar(&o.record, "record", false, "записать замер каждого сервера в журнал")
	f.BoolVar(&o.install, "install", false, "замер каждые 6 часов по расписанию")
	f.BoolVar(&o.uninstall, "uninstall", false, "убрать замер по расписанию")
	return c
}

func tunnelCommand(env *app.Env) *cobra.Command {
	t := &tunnels{env: env}
	c := &cobra.Command{
		Use:   "tunnel",
		Short: "Туннели через ssh: SOCKS, системный прокси, порты, база, expose",
		Run:   func(cmd *cobra.Command, args []string) { printTunnels() },
	}
	up := &cobra.Command{
		Use:   "up [сервер] [--port 1080]",
		Short: "SOCKS5 через сервер (ssh -D): слушает только 127.0.0.1, наружу идёт обычный ssh",
		Args:  cobra.MaximumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return t.up(app.First(args)) },
	}
	up.Flags().IntVar(&t.port, "port", 0, "локальный порт SOCKS (по умолчанию tunnel.port)")
	check := &cobra.Command{
		Use:   "check",
		Short: "Внешний IP напрямую и через SOCKS",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { t.check() },
	}
	check.Flags().IntVar(&t.port, "port", 0, "локальный порт SOCKS")
	system := &cobra.Command{
		Use:       "system on|off [сервер]",
		Short:     "Пустить весь системный трафик через SOCKS (настройки прокси macOS)",
		Args:      cobra.RangeArgs(1, 2),
		ValidArgs: []string{"on", "off"},
		RunE:      func(cmd *cobra.Command, args []string) error { return t.system(args) },
	}
	system.Flags().IntVar(&t.port, "port", 0, "локальный порт SOCKS")
	var via string
	db := &cobra.Command{
		Use:               "db [проект] [--via сервер] [--port L]",
		Short:             "Туннель к базе проекта для GUI-клиента; живёт до tunnel down",
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return t.database(app.First(args), via) },
	}
	db.Flags().StringVar(&via, "via", "", "сервер вместо ssh проекта")
	db.Flags().IntVar(&t.port, "port", 0, "локальный порт (по умолчанию свободный)")
	var all allOptions
	allCmd := &cobra.Command{
		Use:   "all [сервер|off] [--no-dns] [-x подсеть...]",
		Short: "Весь трафик через ssh без настройки прокси (sshuttle); off - выключить",
		Long: `Прозрачный туннель поверх ssh: sshuttle перехватывает весь IPv4 и DNS и отправляет через сервер.
Приложениям ничего настраивать не надо, на сервере нужен только python3.
Мимо туннеля остаются сам сервер и частные сети (локальная сеть и ресурсы корпоративного VPN).
IPv6 sshuttle не перехватывает. sudo спросит пароль при старте.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return t.all(app.First(args), all) },
	}
	allCmd.Flags().BoolVar(&all.noDNS, "no-dns", false, "DNS не заворачивать")
	allCmd.Flags().StringSliceVarP(&all.exclude, "exclude", "x", nil, "ещё подсети или адреса мимо туннеля")
	c.AddCommand(allCmd, up, &cobra.Command{
		Use:   "down [имя|all]",
		Short: "Снять туннель; без имени - SOCKS",
		Args:  cobra.MaximumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return t.down(app.First(args)) },
	}, check, system, &cobra.Command{
		Use:   "port <L:host:R | L:R | R> [сервер]",
		Short: "Пробросить порт с сервера на 127.0.0.1 (ssh -L)",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  func(cmd *cobra.Command, args []string) error { return t.forward(args) },
	}, db, exposeCommand(env))
	return c
}

func exposeCommand(env *app.Env) *cobra.Command {
	var o exposeOptions
	c := &cobra.Command{
		Use:   "expose <порт> [--frp host:port | --ssh сервер] [--remote N]",
		Short: "Показать локальный порт наружу: cloudflared, frpc или ssh -R; Ctrl-C снимает",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return expose(env, args[0], o) },
	}
	c.Flags().StringVar(&o.frp, "frp", "", "сервер frps host[:port]")
	c.Flags().StringVar(&o.ssh, "ssh", "", "сервер для ssh -R")
	c.Flags().IntVar(&o.remote, "remote", 0, "порт снаружи (по умолчанию тот же)")
	return c
}

func dbCommand(env *app.Env) *cobra.Command {
	var via string
	c := &cobra.Command{
		Use:               "db [имя] [dump|restore <файл>]",
		Short:             "Клиент, дамп или восстановление базы по .env проекта",
		Long:              "С --via <алиас> база открывается через ssh-туннель к серверу: локальный порт подбирается, туннель снимается на выходе.",
		ValidArgsFunction: app.ProjectNames,
		RunE:              func(cmd *cobra.Command, args []string) error { return database(env, args, via) },
	}
	c.Flags().StringVar(&via, "via", "", "ssh-алиас или проект: открыть базу через туннель к серверу")
	return c
}

func hookCommand(env *app.Env) *cobra.Command {
	var o hookOptions
	c := &cobra.Command{
		Use:   "hook [порт] [--status 200] [--body файл] [--expose]",
		Short: "Приёмник вебхуков: каждый запрос разобранным, журнал и повтор в свой сервер",
		Long: `Принимает любые запросы на порт (по умолчанию 9999) и печатает их разобранными: заголовки,
JSON с отступами, поля формы. Всё пишется в журнал - tim hook replay <n> --to <адрес> повторит
запрос в dev-сервер. --expose даёт публичный адрес через cloudflared (как tim tunnel expose).
Данные платёжки не уходят третьему сервису, как с webhook.site.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return catchHooks(env, args, o) },
	}
	c.Flags().IntVar(&o.status, "status", 200, "каким кодом отвечать")
	c.Flags().StringVar(&o.body, "body", "", "файл с телом ответа")
	c.Flags().BoolVar(&o.expose, "expose", false, "публичный адрес через cloudflared")
	c.Flags().BoolVar(&o.list, "list", false, "что лежит в журнале")
	var to string
	replay := &cobra.Command{
		Use:   "replay <n> --to http://localhost:8000",
		Short: "Повторить запрос из журнала в свой сервер",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return replayHook(args[0], to) },
	}
	replay.Flags().StringVar(&to, "to", "", "адрес своего сервера")
	c.AddCommand(replay)
	return c
}
