package cmd

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/tui"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var vpnCmd = &cobra.Command{
	Use:   "vpn",
	Short: "Что с VPN: туннели, подключения, куда идёт трафик, DNS, цели обхода",
	RunE: func(cmd *cobra.Command, args []string) error {
		if tui.Available() {
			return runScreen(vpnScreen())
		}
		tunnels := netinfo.Tunnels()
		hasV6 := false
		if len(tunnels) == 0 {
			ui.Println(ui.Dim("туннелей нет"))
		}
		for _, t := range tunnels {
			addrs := append(append([]string{}, t.IPv4...), t.IPv6...)
			hasV6 = hasV6 || len(t.IPv6) > 0
			ui.Println(ui.Label(ui.Pad(t.Iface, 8)) + strings.Join(addrs, " "))
		}
		for _, c := range netinfo.NetConns() {
			if c.State == "Connected" {
				ui.Println(ui.Label("подключение ") + c.Name)
			}
		}
		inet := netinfo.InternetIface()
		switch {
		case netinfo.IsTunnelIface(inet):
			ui.Println(ui.Warn("весь трафик в интернет идёт в туннель " + inet))
		case len(tunnels) > 0:
			ui.Println(ui.Info("раздельный туннель: интернет идёт через " + ui.Dash(inet)))
		default:
			ui.Println(ui.Dim("интернет напрямую через " + ui.Dash(inet)))
		}
		if dns := netinfo.DNSServers(); len(dns) > 0 {
			ui.Println(ui.Label("dns ") + strings.Join(dns, " "))
		}
		phys, gw := bypass.Physical()
		total, ok := 0, 0
		for _, h := range cfg.Hosts {
			for _, d := range bypass.ExpandAddr(h.Addr) {
				total++
				if _, bad := bypass.RouteState(d, phys, gw); !bad && bypass.Bypassed(d, phys, gw) {
					ok++
				}
			}
		}
		if total > 0 {
			line := ui.Label("обход ") + strconv.Itoa(ok) + " из " + strconv.Itoa(total) + " адресов"
			if ok < total {
				line += "  " + ui.Dim("tim vpn fix")
			}
			ui.Println(line)
			ui.Println(bypass.GuardLine())
		}
		if hasV6 {
			ui.Println(ui.Dim("обход работает только для IPv4; IPv6 через туннель остаётся как есть"))
		}
		return nil
	},
}

var vpnExcludeCmd = &cobra.Command{
	Use:   "exclude <адрес|домен|подсеть|as:N...> [--owner] [--label x] | --watch [сайт]",
	Short: "Пустить мимо VPN: то же, что route add; --watch подсмотрит, куда ходит сайт, и предложит сети",
	Long: `Цель - адрес, домен, подсеть или as:N; --owner берёт все сети владельца домена.
Сайт часто грузит плеер, CDN и счётчики с чужих сетей, которых по адресу не угадать: --watch
полминуты смотрит, какие соединения ушли в туннель, пока ты открываешь сайт, и складывает их
по владельцам сетей - остаётся отметить нужные. Аргумент при --watch - только пометка целей.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if vpnWatchOn {
			label := vpnExcludeOpts.label
			if label == "" && len(args) == 1 {
				label = args[0]
			}
			return vpnWatch(vpnWatchSec, label, vpnExcludeOpts)
		}
		if len(args) == 0 {
			return ui.Errorf("что пустить мимо VPN? адрес, домен, подсеть, as:N или --watch")
		}
		return routeAdd(args, vpnExcludeOpts)
	},
}

var (
	vpnExcludeOpts addOpts
	vpnWatchOn     bool
	vpnWatchSec    int
)

var vpnFixCmd = &cobra.Command{
	Use:   "fix",
	Short: "После смены сети: снять протухшие маршруты и завести все цели заново",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := bypass.Clean(cfg, false); err != nil {
			return err
		}
		dests, err := bypass.CollectDests(cfg, nil)
		if err != nil {
			return err
		}
		return bypass.Up(cfg, dests)
	},
}

func init() {
	vpnExcludeOpts.bind(vpnExcludeCmd)
	vpnExcludeCmd.Flags().BoolVar(&vpnWatchOn, "watch", false, "посмотреть, куда ходит сайт через туннель, и выбрать сети")
	vpnExcludeCmd.Flags().IntVar(&vpnWatchSec, "seconds", 20, "сколько секунд смотреть (с --watch)")
	vpnCmd.AddCommand(vpnExcludeCmd, vpnFixCmd)
	root.AddCommand(vpnCmd)
}
