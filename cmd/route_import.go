package cmd

import (
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/vpnhelp"
)

var (
	importList    string
	importScript  string
	importByOwner bool
)

// vpnhelpItems: всё, что держал старый демон: зашитые в скрипт сети и свой список.
func vpnhelpItems() []vpnhelp.Item {
	var items []vpnhelp.Item
	if data, err := os.ReadFile(importScript); err == nil {
		items = append(items, vpnhelp.ParseScript(string(data))...)
	}
	if data, err := os.ReadFile(importList); err == nil {
		items = append(items, vpnhelp.ParseList(string(data))...)
	}
	return items
}

// covered: сеть целиком лежит внутри одной из сетей списка.
func covered(cidr string, nets []string) bool {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}
	for _, n := range nets {
		q, err := netip.ParsePrefix(n)
		if err == nil && q.Bits() <= p.Bits() && q.Contains(p.Addr()) {
			return true
		}
	}
	return false
}

// byOwner заменяет сети на AS их владельцев; сети, которые владелец сейчас не анонсирует, остаются как есть.
func byOwner(items []vpnhelp.Item) []config.Host {
	var hosts []config.Host
	asLabel := map[string]string{}
	var asOrder []string
	var nets []vpnhelp.Item
	for _, it := range items {
		t, err := netinfo.ParseTarget(it.Addr)
		if err != nil || t.Kind != netinfo.KindNet {
			hosts = append(hosts, config.Host{Addr: it.Addr, Label: it.Label})
			continue
		}
		nets = append(nets, vpnhelp.Item{Addr: t.Value, Label: it.Label})
		p, _ := netip.ParsePrefix(t.Value)
		origins, err := asn.Origins([]string{p.Addr().Next().String()})
		if err != nil {
			continue
		}
		for _, a := range origins {
			if _, ok := asLabel[a]; !ok {
				name := asn.Holder(a)
				asLabel[a] = strings.TrimSpace(strings.SplitN(name, " ", 2)[0])
				asOrder = append(asOrder, a)
			}
		}
	}
	var announced []string
	for _, a := range asOrder {
		list, err := asn.Prefixes(stateDir(), a)
		if err != nil {
			continue
		}
		announced = append(announced, list...)
		hosts = append(hosts, config.Host{Addr: "as:" + a, Label: strings.ToLower(asLabel[a])})
	}
	for _, n := range nets {
		if !covered(n.Addr, announced) {
			hosts = append(hosts, config.Host{Addr: n.Addr, Label: n.Label})
		}
	}
	return hosts
}

var routeImportCmd = &cobra.Command{
	Use:   "import [--by-owner] [--list файл] [--script файл]",
	Short: "Перенести цели обхода из vpn-help: сети из скрипта и свой список",
	Long: `Забирает сети, зашитые в vpn-bypass-routes.sh, и строки из vpn-bypass.list в цели tim.
--by-owner заменяет сети на автономные системы владельцев (as:N): их сети страж обновляет сам,
а список, вписанный руками, молча устаревает. Повторы пропускаются.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error { return routeImport(importByOwner) },
}

func routeImport(byOwnerAS bool) error {
	items := vpnhelpItems()
	if len(items) == 0 {
		return ui.Errorf("нечего переносить: нет ни %s, ни %s", importScript, importList)
	}
	var hosts []config.Host
	if byOwnerAS {
		hosts = byOwner(items)
	} else {
		for _, it := range items {
			label := it.Label
			if label == "" {
				label = "vpn-help"
			}
			hosts = append(hosts, config.Host{Addr: it.Addr, Label: label})
		}
	}
	added := 0
	for _, h := range hosts {
		t, err := netinfo.ParseTarget(h.Addr)
		if err != nil {
			ui.Println(ui.Warn(h.Addr + ": " + err.Error()))
			continue
		}
		h.Addr = t.Value
		if cfg.AddHost(h) {
			added++
			ui.Println(ui.Label("добавлен ") + h.Addr + ui.Dim(" "+h.Label))
		}
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	ui.Println(ui.Dim("перенесено " + strconv.Itoa(added) + " из " + strconv.Itoa(len(hosts)) + "; маршруты поставит tim route up или страж"))
	return nil
}

func init() {
	routeImportCmd.Flags().StringVar(&importList, "list", vpnhelp.List, "свой список vpn-help")
	routeImportCmd.Flags().StringVar(&importScript, "script", vpnhelp.Script, "скрипт vpn-help с сетями")
	routeImportCmd.Flags().BoolVar(&importByOwner, "by-owner", false, "заменить сети на AS их владельцев")
	routeCmd.AddCommand(routeImportCmd)
}
