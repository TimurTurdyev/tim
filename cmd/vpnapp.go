package cmd

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// tim vpn app: приложение целиком мимо VPN. Таблица маршрутов приложений не различает, поэтому
// через pf: трафик процессов группы novpn уходит в физический интерфейс (route-to) с его
// адресом (nat). Приложение запускается под этой группой из tim; из Dock оно пойдёт как обычно.

var (
	appOff    bool
	appStatus bool
	appCheck  bool
)

var vpnAppCmd = &cobra.Command{
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
		case appOff:
			return bypass.AppOff()
		case appStatus:
			return bypass.AppStatus()
		case appCheck:
			return bypass.AppCheck()
		case len(args) == 0:
			return ui.Errorf("какое приложение? tim vpn app Firefox, или --check, --status, --off")
		}
		return bypass.AppLaunch(args[0], closeRunningApp)
	},
}

func init() {
	vpnAppCmd.Flags().BoolVar(&appOff, "off", false, "снять правило pf")
	vpnAppCmd.Flags().BoolVar(&appStatus, "status", false, "правила и кто сейчас работает мимо VPN")
	vpnAppCmd.Flags().BoolVar(&appCheck, "check", false, "завести правило и проверить его на curl")
	vpnCmd.AddCommand(vpnAppCmd)
}

// closeRunningApp бережно закрывает открытый экземпляр приложения перед запуском мимо VPN.
func closeRunningApp(bundle, appName string) error {
	list, err := machine.All()
	if err != nil {
		return nil
	}
	for _, a := range procs.Find(list, appName) {
		if a.Main.Comm != bundle && !strings.HasPrefix(a.Main.Comm, bundle+"/") {
			continue
		}
		ui.Println(ui.Dim("закрываю " + a.Name + ": открытый экземпляр остался бы под VPN"))
		text, err := machine.Close(a, false)
		if err != nil {
			return ui.Errorf("%v; закрой %s сам и повтори", err, a.Name)
		}
		ui.Println(ui.Dim(text))
		time.Sleep(time.Second)
	}
	return nil
}
