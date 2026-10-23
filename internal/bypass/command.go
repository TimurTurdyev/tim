package bypass

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
)

// addOptions: флаги новой цели обхода.
type addOptions struct {
	owner, noUp, force bool
	label              string
}

func (o *addOptions) bind(c *cobra.Command) {
	c.Flags().StringVar(&o.label, "label", "", "пометка (для одной цели)")
	c.Flags().BoolVar(&o.noUp, "no-up", false, "только сохранить, маршрут не заводить")
	c.Flags().BoolVar(&o.force, "force", false, "разрешить частные адреса, подсети и CDN")
	c.Flags().BoolVar(&o.owner, "owner", false, "для домена - все сети его владельца (AS), а не адреса домена")
}

// unblockOptions: сколько кругов смотреть, куда ходит сайт, и что учитывать.
type unblockOptions struct {
	rounds, seconds int
	all             bool
}

func (o *unblockOptions) bind(c *cobra.Command) {
	c.Flags().IntVar(&o.rounds, "rounds", 3, "сколько кругов смотреть, куда ходит сайт")
	c.Flags().IntVar(&o.seconds, "seconds", 15, "сколько секунд смотреть за круг")
	c.Flags().BoolVarP(&o.all, "all", "a", false, "учитывать все программы, а не только браузер")
}

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	return []*cobra.Command{directCommand(env), routeCommand(env), vpnCommand(env), proxyCommand()}
}

func directCommand(env *app.Env) *cobra.Command {
	var o directOptions
	c := &cobra.Command{
		Use:   "direct [сайт|приложение|сеть...]",
		Short: "Пустить напрямую, мимо VPN: сайт целиком, приложение или сеть",
		Long: `Сайт (example.org) - мимо VPN идёт он сам и всё, что он подгружает: плеер, видео, счётчики.
tim открывает сайт в браузере, смотрит, куда тот ходит через туннель, и добавляет эти сети, пока
новые не кончатся. С --owner, --label или --no-up - только сети владельца домена, без браузера.
Приложение (Firefox, "Google Chrome", путь к .app, на Linux - команда) - напрямую идёт весь его
трафик: программа запускается под группой novpn, а её соединения ядро уводит в физический
интерфейс. Уже открытый экземпляр закрывается бережно и запускается заново.
Сеть (as:64500, 203.0.113.5, 10.1.0.0/16) - цель в конфиг и маршрут через физический шлюз.
Без аргументов - что сейчас идёт напрямую, без пароля; правила ядра для приложений - с --rules.
Где нужны права администратора, пароль sudo спрашивается один раз.
Вернуть под VPN: tim direct rm <цель|пометка>, приложения - tim direct off.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return direct(env, args, o) },
	}
	o.add.bind(c)
	o.unblock.bind(c)
	c.Flags().BoolVar(&o.rules, "rules", false, "без аргументов: показать и правила ядра (pf, netfilter) - спросит пароль")
	c.AddCommand(
		&cobra.Command{
			Use:   "rm <цель|пометка...>",
			Short: "Вернуть под VPN: снять цели и их маршруты",
			Args:  cobra.MinimumNArgs(1),
			RunE:  func(cmd *cobra.Command, args []string) error { return removeTargets(env, args) },
		},
		&cobra.Command{
			Use:   "list",
			Short: "Цели обхода из конфига",
			Args:  cobra.NoArgs,
			Run:   func(cmd *cobra.Command, args []string) { printTargets(env.Cfg) },
		},
		directWatchCommand(env),
		&cobra.Command{
			Use:   "check",
			Short: "Завести правила для приложений и проверить на curl, какую страну видят их процессы",
			Args:  cobra.NoArgs,
			RunE:  func(cmd *cobra.Command, args []string) error { return AppCheck() },
		},
		&cobra.Command{
			Use:   "off",
			Short: "Снять правила обхода по приложениям и прокси",
			Args:  cobra.NoArgs,
			RunE:  func(cmd *cobra.Command, args []string) error { return AppOff() },
		},
	)
	return c
}

func directWatchCommand(env *app.Env) *cobra.Command {
	var o addOptions
	var seconds int
	c := &cobra.Command{
		Use:   "watch [пометка]",
		Short: "Подсмотреть, куда ходит сайт или программа через туннель, и выбрать сети",
		Long: `Сайт часто грузит плеер, CDN и счётчики с чужих сетей, которых по адресу не угадать. tim смотрит,
какие соединения ушли в туннель, пока ты открываешь сайт, и складывает их по владельцам сетей -
остаётся отметить нужные. Аргумент - пометка новых целей, обычно сам сайт.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return chooseNets(env, seconds, watchLabel(o, args), o)
		},
	}
	o.bind(c)
	c.Flags().IntVar(&seconds, "seconds", 20, "сколько секунд смотреть")
	return c
}

func routeCommand(env *app.Env) *cobra.Command {
	var list bool
	c := &cobra.Command{
		Use:   "route",
		Short: "Маршруты мимо VPN: поднять, снять, починить после смены сети, страж",
		Long: `Маршрут к каждой цели из конфига ([[host]]) идёт через шлюз физического интерфейса, поэтому
трафик к ней минует туннель VPN. Домен раскрывается во все A-записи в момент up, as:N - во все
сети автономной системы. Заводить и убирать цели: tim direct и tim direct rm.`,
		Args: cobra.NoArgs,
		Run:  func(cmd *cobra.Command, args []string) { printRoutes(env.Cfg, false, list) },
	}
	c.Flags().BoolVarP(&list, "all", "a", false, "все адреса, а не только с отклонением")
	var all bool
	clean := &cobra.Command{
		Use:   "clean [--all]",
		Short: "Снять протухшие маршруты на прежний шлюз после смены сети",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return Clean(env.Cfg, all) },
	}
	clean.Flags().BoolVarP(&all, "all", "a", false, "по всей таблице, а не только для целей из конфига")
	c.AddCommand(
		&cobra.Command{
			Use:   "check",
			Short: "Состояние плюс проверка порта 22 у адресов",
			Args:  cobra.NoArgs,
			Run:   func(cmd *cobra.Command, args []string) { printRoutes(env.Cfg, true, true) },
		},
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
			Use:   "fix",
			Short: "После смены сети: снять протухшие маршруты и завести все цели заново",
			Args:  cobra.NoArgs,
			RunE:  func(cmd *cobra.Command, args []string) error { return fix(env) },
		},
		&cobra.Command{
			Use:   "table",
			Short: "Статические маршруты таблицы (адреса и подсети)",
			Args:  cobra.NoArgs,
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

// watchCommand: скрыт, потому что его запускает служба стража; имя менять нельзя - сломаются установленные.
func watchCommand(env *app.Env) *cobra.Command {
	var every time.Duration
	var once bool
	c := &cobra.Command{
		Use:    "watch [--every 10s] [--once]",
		Short:  "Страж: держать маршруты обхода живыми при смене сети и переподключении VPN",
		Hidden: true,
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
	return &cobra.Command{
		Use:   "vpn",
		Short: "Что с VPN: туннели, подключения, куда идёт трафик, DNS, цели обхода",
		Long: `В терминале - экран: туннель, страж и состояние каждой цели обхода. В скрипте или через
пайп - та же сводка текстом. Пустить мимо VPN - tim direct, маршруты и страж - tim route.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Interactive() {
				return env.Open(Screen(env))
			}
			printVPN(env.Cfg)
			return nil
		},
	}
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
