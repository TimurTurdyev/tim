package cmd

import (
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// tim direct: одна команда на весь обход VPN. Что именно пустить напрямую, tim понимает по аргументу:
// сеть по форме, приложение по тому, что оно установлено, остальное с точкой - сайт.

var (
	directWatch   bool
	directCheck   bool
	directStatus  bool
	directOff     bool
	directYes     bool
	directAll     bool
	directSeconds int
	directRounds  int
	directOpts    addOpts
)

var directCmd = &cobra.Command{
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
	RunE: func(cmd *cobra.Command, args []string) error {
		switch {
		case directStatus:
			if err := bypass.AppStatus(); err != nil {
				return err
			}
			ui.Println()
			return routeStatus(false)
		case directOff:
			return bypass.AppOff()
		case directCheck:
			return bypass.AppCheck()
		case directWatch:
			label := directOpts.label
			if label == "" && len(args) == 1 {
				label = args[0]
			}
			return vpnWatch(directSeconds, label, directOpts)
		case len(args) == 0:
			return ui.Errorf("что пустить напрямую? сайт (example.com), приложение (Firefox) или сеть (as:N, адрес, подсеть)")
		}
		switch bypass.Classify(args[0], bypass.AppExists) {
		case bypass.KindApp:
			if len(args) > 1 {
				return ui.Errorf("приложение - по одному: tim direct %s", args[0])
			}
			return bypass.AppLaunch(args[0], closeRunningApp)
		case bypass.KindSite:
			if len(args) > 1 || directOpts.owner || directOpts.noUp || directOpts.force || directOpts.label != "" {
				return routeAdd(args, directOpts)
			}
			unblockRounds, unblockSeconds, unblockYes, unblockAll = directRounds, directSeconds, directYes, directAll
			return vpnUnblock(args[0])
		case bypass.KindNet:
			return routeAdd(args, directOpts)
		}
		return ui.Errorf("не понял %s: сайт пишется с точкой, приложение - как оно называется в системе, сеть - as:N, адрес или подсеть", args[0])
	},
}

var directRmCmd = &cobra.Command{
	Use:   "rm <цель|пометка...>",
	Short: "Вернуть под VPN: снять цели и их маршруты",
	Args:  cobra.MinimumNArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return routeRmCmd.RunE(cmd, args) },
}

func init() {
	directOpts.bind(directCmd)
	directCmd.Flags().BoolVar(&directWatch, "watch", false, "посмотреть, куда ходит сайт или программа, и выбрать сети")
	directCmd.Flags().BoolVar(&directStatus, "status", false, "что сейчас идёт напрямую: приложения, правила, цели")
	directCmd.Flags().BoolVar(&directOff, "off", false, "снять правила обхода по приложениям")
	directCmd.Flags().BoolVar(&directCheck, "check", false, "завести правила для приложений и проверить их на curl")
	directCmd.Flags().IntVar(&directSeconds, "seconds", 15, "сколько секунд смотреть за круг")
	directCmd.Flags().IntVar(&directRounds, "rounds", 3, "сколько кругов для сайта")
	directCmd.Flags().BoolVarP(&directYes, "yes", "y", false, "не спрашивать после каждого круга")
	directCmd.Flags().BoolVar(&directAll, "all", false, "учитывать все программы, а не только браузер")
	directCmd.AddCommand(directRmCmd)
	root.AddCommand(directCmd)
}
