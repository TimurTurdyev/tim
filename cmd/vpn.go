package cmd

import (
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var vpnCmd = &cobra.Command{
	Use:   "vpn",
	Short: "Что с VPN: туннели, подключения, куда идёт трафик, DNS, цели обхода",
	RunE: func(cmd *cobra.Command, args []string) error {
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
			ui.Println(ui.Info("раздельный туннель: интернет идёт через " + dashIfEmpty(inet)))
		default:
			ui.Println(ui.Dim("интернет напрямую через " + dashIfEmpty(inet)))
		}
		if dns := netinfo.DNSServers(); len(dns) > 0 {
			ui.Println(ui.Label("dns ") + strings.Join(dns, " "))
		}
		phys, gw := physical()
		total, ok := 0, 0
		for _, h := range cfg.Hosts {
			for _, d := range expandAddr(h.Addr) {
				total++
				if _, bad := routeState(d, phys, gw); !bad && bypassed(d, phys, gw) {
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
			ui.Println(guardLine())
		}
		if hasV6 {
			ui.Println(ui.Dim("обход работает только для IPv4; IPv6 через туннель остаётся как есть"))
		}
		return nil
	},
}

var vpnExcludeCmd = &cobra.Command{
	Use:   "exclude <адрес|домен|подсеть|as:N...> [--owner] [--label x]",
	Short: "Пустить мимо VPN: то же, что route add",
	Args:  cobra.MinimumNArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return routeAddCmd.RunE(cmd, args) },
}

var vpnFixCmd = &cobra.Command{
	Use:   "fix",
	Short: "После смены сети: снять протухшие маршруты и завести все цели заново",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := routeClean(false); err != nil {
			return err
		}
		dests, err := collectDests(nil)
		if err != nil {
			return err
		}
		return routeUp(dests)
	},
}

func init() {
	vpnExcludeCmd.Flags().StringVar(&routeLabel, "label", "", "пометка (для одной цели)")
	vpnExcludeCmd.Flags().BoolVar(&routeNoUp, "no-up", false, "только сохранить")
	vpnExcludeCmd.Flags().BoolVar(&routeForce, "force", false, "разрешить частные адреса и подсети")
	vpnExcludeCmd.Flags().BoolVar(&routeOwner, "owner", false, "для домена - все сети его владельца (AS)")
	vpnCmd.AddCommand(vpnExcludeCmd, vpnFixCmd)
	root.AddCommand(vpnCmd)
}
