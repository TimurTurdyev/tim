package machine

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/app"
)

// Commands: команды раздела.
func Commands(env *app.Env) []*cobra.Command {
	return []*cobra.Command{
		hwCommand(env), topCommand(env), psCommand(env), killCommand(),
		portsCommand(env), portCommand(env), netCommand(), statsCommand(), ipCommand(env),
	}
}

func hwCommand(env *app.Env) *cobra.Command {
	var watch bool
	c := &cobra.Command{
		Use:   "hw [--watch]",
		Short: "Железо и его загрузка: процессор, видеокарта, память, swap, диск, экраны, тепло",
		Long: `Паспорт машины и текущая загрузка. У видеокарты видно, насколько она занята и сколько видеопамяти
в работе, и какие твои процессы её держат (сколько каждый грузит - без root не узнать).
--watch обновляет процессор, видеокарту и память раз в секунду; выход - Ctrl-C.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if watch && app.Interactive() {
				return env.Open(HWScreen())
			}
			// Без терминала живому экрану негде жить - один снимок.
			return printSnapshot()
		},
	}
	c.Flags().BoolVarP(&watch, "watch", "w", false, "обновлять раз в секунду")
	c.AddCommand(&cobra.Command{
		Use:   "battery",
		Short: "Износ батареи: циклы, ёмкость от заводской, состояние",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { printBattery() },
	})
	return c
}

func topCommand(env *app.Env) *cobra.Command {
	var o topOptions
	c := &cobra.Command{
		Use:   "top [--by app|project] [--cpu] [-n 10]",
		Short: "Кто занял память и CPU: по приложениям или по проектам",
		Long: `Складывает процессы по приложению (все помощники браузера - в одну строку) или по проекту
реестра (по рабочей папке процесса). Память - RSS: общие страницы считаются в каждом процессе,
поэтому у многопроцессных приложений сумма выше, чем в Мониторе системы.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Interactive() && o.by == "app" {
				return env.Open(ProcsScreen(""))
			}
			return printTop(env.Cfg, o)
		},
	}
	c.Flags().StringVar(&o.by, "by", "app", "app или project")
	c.Flags().IntVarP(&o.count, "count", "n", 10, "сколько строк")
	c.Flags().BoolVar(&o.cpu, "cpu", false, "сортировать по CPU, а не по памяти")
	return c
}

func psCommand(env *app.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "ps <имя>",
		Short: "Найти процессы по имени (с опечатками) и показать, как их закрыть",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if app.Interactive() {
				return env.Open(ProcsScreen(args[0]))
			}
			return printFound(args[0])
		},
	}
}

func killCommand() *cobra.Command {
	var o killOptions
	c := &cobra.Command{
		Use:   "kill <имя|pid> [--force] [-y]",
		Short: "Закрыть зависшее приложение или процесс: сначала бережно, потом принудительно",
		Long: `Ищет по имени с опечатками ("goglehrome" найдёт Google Chrome) и закрывает все процессы
приложения разом. Приложение сначала закрывается как Cmd+Q - оно успеет спросить про несохранённое.
Не закрылось за несколько секунд - tim спросит, убить ли принудительно. Обычная программа сначала
получает SIGTERM, потом SIGKILL. Системные процессы (launchd, WindowServer и т.п.) не трогаются.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return kill(args[0], o) },
	}
	c.Flags().BoolVar(&o.force, "force", false, "сразу принудительно (SIGKILL)")
	c.Flags().BoolVarP(&o.yes, "yes", "y", false, "не спрашивать")
	return c
}

func portsCommand(env *app.Env) *cobra.Command {
	var o portsOptions
	c := &cobra.Command{
		Use:   "ports [--all] [--old [--hours 8] [--kill]]",
		Short: "Кто слушает TCP-порты: процесс, сколько работает, чей проект",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return printPorts(env.Cfg, o) },
	}
	c.Flags().BoolVar(&o.all, "all", false, "показать и системные процессы")
	c.Flags().BoolVar(&o.old, "old", false, "dev-серверы проектов, которые работают дольше --hours")
	c.Flags().Float64Var(&o.hours, "hours", 8, "сколько часов считать долгим")
	c.Flags().BoolVar(&o.kill, "kill", false, "с --old: предложить остановить каждый")
	c.Flags().BoolVar(&o.force, "force", false, "SIGKILL вместо SIGTERM")
	return c
}

func portCommand(env *app.Env) *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "port <n> [kill] | port check <host:port...>",
		Short: "Кто держит порт, остановить его; проверка чужих портов",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return portOwner(env.Cfg, args, force)
		},
	}
	c.Flags().BoolVar(&force, "force", false, "SIGKILL вместо SIGTERM")
	c.AddCommand(&cobra.Command{
		Use:   "check <host:port...>",
		Short: "Открыт ли TCP-порт и за сколько",
		Args:  cobra.MinimumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return checkPorts(args) },
	})
	return c
}

func netCommand() *cobra.Command {
	c := &cobra.Command{Use: "net", Short: "Сеть: проверка по шагам"}
	check := &cobra.Command{
		Use:   "check",
		Short: "Шлюз, интернет, DNS, внешний IP, VPN: что именно не работает",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return checkNet() },
	}
	var seconds, count int
	top := &cobra.Command{
		Use:   "top [--seconds 2] [-n 10]",
		Short: "Кто сейчас качает и отдаёт: процессы по трафику за пару секунд",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return printNetTop(seconds, count) },
	}
	top.Flags().IntVar(&seconds, "seconds", 2, "сколько секунд мерить")
	top.Flags().IntVarP(&count, "count", "n", 10, "сколько строк")
	wifi := &cobra.Command{
		Use:   "wifi",
		Short: "Wi-Fi: сеть, канал, стандарт, скорость, сигнал и шум",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return printWifi() },
	}
	c.AddCommand(check, top, wifi)
	return c
}

func statsCommand() *cobra.Command {
	var o statsOptions
	c := &cobra.Command{
		Use:   "stats [--month] [--record] [--install] [--uninstall]",
		Short: "История машины: место, память, нагрузка, батарея и когда кончится диск",
		Long: `Показывает графики за неделю (--month - за месяц) по журналу замеров и прогноз свободного места.
Замеры пишет tim stats --record; tim stats --install ставит его каждый час (launchd, на Linux - systemd).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error { return runStats(o) },
	}
	c.Flags().BoolVar(&o.record, "record", false, "сделать замер и дописать в журнал")
	c.Flags().BoolVar(&o.install, "install", false, "замер каждый час")
	c.Flags().BoolVar(&o.uninstall, "uninstall", false, "убрать ежечасный замер")
	c.Flags().BoolVar(&o.month, "month", false, "за месяц, а не за неделю")
	return c
}

func ipCommand(env *app.Env) *cobra.Command {
	return &cobra.Command{
		Use:   "ip",
		Short: "Внешний IP: страна, город, провайдер; напрямую и через SOCKS tim",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return printIP(env.Cfg.Tunnel.Port) },
	}
}
