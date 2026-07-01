package bypass

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// addOptions: флаги добавления цели; у route add, vpn exclude и direct свои экземпляры.
type addOptions struct {
	owner, noUp, force bool
	label              string
}

// unblockOptions: сколько кругов смотреть, куда ходит сайт, и что учитывать.
type unblockOptions struct {
	rounds, seconds int
	yes, all        bool
}

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	return []*cobra.Command{routeCommand(env), vpnCommand(env), directCommand(env), proxyCommand()}
}

func (o *addOptions) bind(c *cobra.Command) {
	c.Flags().StringVar(&o.label, "label", "", "пометка (для одной цели)")
	c.Flags().BoolVar(&o.noUp, "no-up", false, "только сохранить, маршрут не заводить")
	c.Flags().BoolVar(&o.force, "force", false, "разрешить частные адреса, подсети и CDN")
	c.Flags().BoolVar(&o.owner, "owner", false, "для домена - все сети его владельца (AS), а не адреса домена")
}

func routeCommand(env *app.Env) *cobra.Command {
	c := &cobra.Command{
		Use:   "route",
		Short: "Маршруты в обход VPN: адреса, домены и подсети из конфига",
		Long: `Цели обхода хранятся в конфиге ([[host]]): адрес, домен или подсеть CIDR.
Домен раскрывается во все A-записи в момент up. Маршрут заводится через шлюз
физического интерфейса, поэтому трафик к цели идёт мимо туннеля VPN.`,
		Run: func(cmd *cobra.Command, args []string) { printRoutes(env.Cfg, false) },
	}
	var add addOptions
	addCmd := &cobra.Command{
		Use:   "add <адрес|домен|подсеть|as:N...> [--owner] [--label x] [--no-up]",
		Short: "Добавить цель обхода в конфиг и сразу завести маршрут",
		Args:  cobra.MinimumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return addTargets(env, args, add) },
	}
	add.bind(addCmd)
	var all bool
	clean := &cobra.Command{
		Use:   "clean [--all]",
		Short: "Снять протухшие маршруты на прежний шлюз после смены сети",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return Clean(env.Cfg, all) },
	}
	clean.Flags().BoolVar(&all, "all", false, "по всей таблице, а не только для целей из конфига")
	c.AddCommand(
		&cobra.Command{
			Use:   "check",
			Short: "Состояние плюс проверка порта 22 у адресов",
			Run:   func(cmd *cobra.Command, args []string) { printRoutes(env.Cfg, true) },
		},
		addCmd, removeCommand(env, "rm <адрес|домен|подсеть|пометка...>", "Снять маршрут и убрать цель из конфига"),
		&cobra.Command{
			Use:   "up [цель|пометка...]",
			Short: "Завести маршруты через физический шлюз (sudo); без аргументов все цели",
			RunE:  func(cmd *cobra.Command, args []string) error { return up(env, args) },
		},
		&cobra.Command{
			Use:   "down [цель|пометка...]",
			Short: "Снять маршруты (sudo); без аргументов все цели",
			RunE:  func(cmd *cobra.Command, args []string) error { return down(env, args) },
		},
		clean,
		&cobra.Command{
			Use:   "list",
			Short: "Цели обхода из конфига",
			Run:   func(cmd *cobra.Command, args []string) { printTargets(env.Cfg) },
		},
		&cobra.Command{
			Use:   "table",
			Short: "Статические маршруты таблицы (адреса и подсети)",
			Run:   func(cmd *cobra.Command, args []string) { printTable(env.Cfg) },
		},
		&cobra.Command{
			Use:   "status",
			Short: "Как работает страж маршрутов: последняя проверка, починки и их причины",
			Args:  cobra.NoArgs,
			Run:   func(cmd *cobra.Command, args []string) { printGuard() },
		},
		watchCommand(env), installCommand(), uninstallCommand(),
	)
	return c
}

func removeCommand(env *app.Env, use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return removeTargets(env, args) },
	}
}

func watchCommand(env *app.Env) *cobra.Command {
	var every time.Duration
	var once bool
	c := &cobra.Command{
		Use:   "watch [--every 10s] [--once]",
		Short: "Страж: держать маршруты обхода живыми при смене сети и переподключении VPN",
		Long: `Каждый цикл перечитывает цели из конфига и чинит маршрут, если его нет, если он ушёл в туннель
или смотрит на шлюз прошлой сети. Обычно работает службой: sudo tim route install.
Без root каждая починка спросит пароль sudo, поэтому вручную удобен --once.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if once {
				return guardOnce(env)
			}
			return guard(env, every)
		},
	}
	c.Flags().DurationVar(&every, "every", 10*time.Second, "как часто проверять")
	c.Flags().BoolVar(&once, "once", false, "одна проверка и выход")
	return c
}

func installCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Поставить стража маршрутов службой системы: обход VPN сам чинится после смены сети",
		Long: `Ставит tim route watch постоянной службой: на macOS - LaunchDaemon dev.tim.routes, на Linux -
systemd-служба tim-routes. Нужны права root; если забыл sudo, tim спросит и повторит команду сам.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return installGuard() },
	}
}

func uninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Убрать стража маршрутов; сами маршруты и цели остаются",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return uninstallGuard() },
	}
}

func vpnCommand(env *app.Env) *cobra.Command {
	c := &cobra.Command{
		Use:   "vpn",
		Short: "Что с VPN: туннели, подключения, куда идёт трафик, DNS, цели обхода",
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Interactive() {
				return env.Open(Screen(env))
			}
			printVPN(env.Cfg)
			return nil
		},
	}
	c.AddCommand(excludeCommand(env), &cobra.Command{
		Use:   "fix",
		Short: "После смены сети: снять протухшие маршруты и завести все цели заново",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return fix(env) },
	}, unblockCommand(env), appCommand())
	return c
}

func excludeCommand(env *app.Env) *cobra.Command {
	var o addOptions
	var watch bool
	var seconds int
	c := &cobra.Command{
		Use:   "exclude <адрес|домен|подсеть|as:N...> [--owner] [--label x] | --watch [сайт]",
		Short: "Пустить мимо VPN: то же, что route add; --watch подсмотрит, куда ходит сайт, и предложит сети",
		Long: `Цель - адрес, домен, подсеть или as:N; --owner берёт все сети владельца домена.
Сайт часто грузит плеер, CDN и счётчики с чужих сетей, которых по адресу не угадать: --watch
полминуты смотрит, какие соединения ушли в туннель, пока ты открываешь сайт, и складывает их
по владельцам сетей - остаётся отметить нужные. Аргумент при --watch - только пометка целей.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case watch:
				return chooseNets(env, seconds, watchLabel(o, args), o)
			case len(args) == 0:
				return ui.Errorf("что пустить мимо VPN? адрес, домен, подсеть, as:N или --watch")
			}
			return addTargets(env, args, o)
		},
	}
	o.bind(c)
	c.Flags().BoolVar(&watch, "watch", false, "посмотреть, куда ходит сайт через туннель, и выбрать сети")
	c.Flags().IntVar(&seconds, "seconds", 20, "сколько секунд смотреть (с --watch)")
	return c
}

func unblockCommand(env *app.Env) *cobra.Command {
	var o unblockOptions
	c := &cobra.Command{
		Use:   "unblock <сайт> [--rounds 3] [--seconds 15] [-y]",
		Short: "Открыть сайт мимо VPN целиком: сам сайт, его плеер и всё, что он подгружает",
		Long: `Сайт, который пишет "отключите VPN", обычно грузит плеер, видео и скрипты с чужих сетей, и
обхода одного адреса мало. tim пускает мимо VPN сети владельца сайта, открывает сайт в браузере,
смотрит, куда тот ходит через туннель, добавляет эти сети и открывает сайт заново - до трёх кругов,
пока новые сети не кончатся. Общие сети (Cloudflare, Google, Amazon) не трогает: через них ходит
половина интернета. Убрать всё, что добавлено для сайта: tim route rm <сайт>.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return unblock(env, args[0], o) },
	}
	c.Flags().IntVar(&o.rounds, "rounds", 3, "сколько кругов смотреть, куда ходит сайт")
	c.Flags().IntVar(&o.seconds, "seconds", 15, "сколько секунд смотреть за круг")
	c.Flags().BoolVarP(&o.yes, "yes", "y", false, "не спрашивать после каждого круга")
	c.Flags().BoolVar(&o.all, "all", false, "учитывать все программы, а не только браузер")
	return c
}

func appCommand() *cobra.Command {
	var off, status, check bool
	c := &cobra.Command{
		Use:   "app <Firefox|Chrome|имя.app|путь> | --check | --status | --off",
		Short: "Приложение целиком мимо VPN: запустить его так, чтобы весь его трафик шёл напрямую",
		Long: `Маршруты не знают, какое приложение шлёт пакет, поэтому tim делает иначе: заводит группу novpn,
правило pf "процессы этой группы - в физический интерфейс с его адресом" и запускает приложение под
этой группой. Всё, что оно откроет, идёт мимо VPN; остальные программы - как раньше. Открытое из
Dock приложение в группу не попадает: перезапусти его через tim.
Соединения группы pf заворачивает на прозрачный прокси tim, тот ходит к адресату через физический
интерфейс. --check заводит всё и проверяет на curl, какую страну видят процессы группы. --off снимает
правило и прокси. Пароль sudo спрашивается один раз. То же самое: tim direct <приложение>.
На Linux вместо pf - метка netfilter по группе и своя таблица маршрутов, прокси не нужен.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case off:
				return AppOff()
			case status:
				return AppStatus()
			case check:
				return AppCheck()
			case len(args) == 0:
				return ui.Errorf("какое приложение? tim vpn app Firefox, или --check, --status, --off")
			}
			return AppLaunch(args[0])
		},
	}
	c.Flags().BoolVar(&off, "off", false, "снять правило pf")
	c.Flags().BoolVar(&status, "status", false, "правила и кто сейчас работает мимо VPN")
	c.Flags().BoolVar(&check, "check", false, "завести правило и проверить его на curl")
	return c
}

func directCommand(env *app.Env) *cobra.Command {
	var o directOptions
	c := &cobra.Command{
		Use:   "direct <сайт|приложение|сеть...> | --watch [пометка] | --check | --status | --off",
		Short: "Пустить напрямую, мимо VPN: сайт целиком, приложение или сеть",
		Long: `Сайт (rick-i-morty.club) - мимо VPN идёт он сам и всё, что он подгружает: плеер, видео, счётчики.
tim открывает сайт в браузере, смотрит, куда тот ходит через туннель, и добавляет эти сети, пока
новые не кончатся. С --owner, --label или --no-up - только сети владельца домена, без браузера.
Приложение (Firefox, "Google Chrome", путь к .app, на Linux - команда) - напрямую идёт весь его
трафик: программа запускается под группой novpn, а её соединения ядро уводит в физический
интерфейс. Уже открытый экземпляр закрывается бережно и запускается заново.
Сеть (as:13238, 203.0.113.5, 10.1.0.0/16) - цель в конфиг и маршрут через физический шлюз.
--watch смотрит, куда ходит сайт или программа, и предлагает сети на выбор. Пароль sudo
спрашивается один раз. Вернуть под VPN: tim direct rm <цель|пометка>, приложения - --off.`,
		RunE: func(cmd *cobra.Command, args []string) error { return direct(env, args, o) },
	}
	o.add.bind(c)
	f := c.Flags()
	f.BoolVar(&o.watch, "watch", false, "посмотреть, куда ходит сайт или программа, и выбрать сети")
	f.BoolVar(&o.status, "status", false, "что сейчас идёт напрямую: приложения, правила, цели")
	f.BoolVar(&o.off, "off", false, "снять правила обхода по приложениям")
	f.BoolVar(&o.check, "check", false, "завести правила для приложений и проверить их на curl")
	f.IntVar(&o.unblock.seconds, "seconds", 15, "сколько секунд смотреть за круг")
	f.IntVar(&o.unblock.rounds, "rounds", 3, "сколько кругов для сайта")
	f.BoolVarP(&o.unblock.yes, "yes", "y", false, "не спрашивать после каждого круга")
	f.BoolVar(&o.unblock.all, "all", false, "учитывать все программы, а не только браузер")
	c.AddCommand(removeCommand(env, "rm <цель|пометка...>", "Вернуть под VPN: снять цели и их маршруты"))
	return c
}

func proxyCommand() *cobra.Command {
	var o proxyOptions
	c := &cobra.Command{
		Use:    "novpn-proxy",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   func(cmd *cobra.Command, args []string) error { return serveProxy(o) },
	}
	c.Flags().IntVar(&o.port, "port", ProxyPort, "порт на 127.0.0.1")
	c.Flags().StringVar(&o.iface, "iface", "", "физический интерфейс")
	c.Flags().StringVar(&o.ip, "ip", "", "его адрес")
	return c
}
