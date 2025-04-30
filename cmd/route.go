package cmd

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/domaudit"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var routeCmd = &cobra.Command{
	Use:   "route",
	Short: "Маршруты в обход VPN: адреса, домены и подсети из конфига",
	Long: `Цели обхода хранятся в конфиге ([[host]]): адрес, домен или подсеть CIDR.
Домен раскрывается во все A-записи в момент up. Маршрут заводится через шлюз
физического интерфейса, поэтому трафик к цели идёт мимо туннеля VPN.`,
	RunE: func(cmd *cobra.Command, args []string) error { return routeStatus(false) },
}

var routeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Состояние плюс проверка порта 22 у адресов",
	RunE:  func(cmd *cobra.Command, args []string) error { return routeStatus(true) },
}

var (
	routeOwner bool
	routeLabel string
	routeNoUp  bool
	routeForce bool
	routeAll   bool
)

var routeAddCmd = &cobra.Command{
	Use:   "add <адрес|домен|подсеть|as:N...> [--owner] [--label x] [--no-up]",
	Short: "Добавить цель обхода в конфиг и сразу завести маршрут",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var dests []string
		for _, a := range args {
			t, err := netinfo.ParseTarget(a)
			if err != nil {
				return err
			}
			targets := []netinfo.Target{t}
			if routeOwner && t.Kind == netinfo.KindDomain {
				domain := t.Value
				var names []string
				if targets, names, err = ownerTargets(domain); err != nil {
					return err
				}
				if routeLabel == "" && len(args) == 1 {
					routeLabel = domain
				}
				for i, o := range targets {
					ui.Println(ui.Dim(domain + ": сети " + o.Value + " " + names[i]))
				}
			}
			for _, t := range targets {
				if netinfo.PrivateTarget(t) && !routeForce {
					return ui.Errorf("%s - частная сеть, она обычно доступна только через VPN; если точно надо: --force", t.Value)
				}
				label := ""
				if len(args) == 1 {
					label = routeLabel
				}
				if cfg.AddHost(config.Host{Addr: t.Value, Label: label}) {
					ui.Println(ui.Label("добавлен ") + t.Value + ui.Dim(" "+label))
				} else {
					ui.Println(ui.Dim(t.Value + ": уже в списке"))
				}
				dests = append(dests, expand(t)...)
			}
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		if routeNoUp {
			return nil
		}
		return routeUp(dests)
	},
}

var routeRmCmd = &cobra.Command{
	Use:   "rm <адрес|домен|подсеть|пометка...>",
	Short: "Снять маршрут и убрать цель из конфига",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var dests []string
		for _, a := range args {
			removed := cfg.RemoveHost(a)
			if len(removed) == 0 {
				if t, err := netinfo.ParseTarget(a); err == nil {
					removed = cfg.RemoveHost(t.Value)
				}
			}
			if len(removed) == 0 {
				return ui.Errorf("в списке нет %s: tim route list", a)
			}
			for _, h := range removed {
				dests = append(dests, expandAddr(h.Addr)...)
				ui.Println(ui.Label("убран ") + h.Addr)
			}
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		routeDown(dests)
		return nil
	},
}

var routeUpCmd = &cobra.Command{
	Use:   "up [цель|пометка...]",
	Short: "Завести маршруты через физический шлюз (sudo); без аргументов все цели",
	RunE: func(cmd *cobra.Command, args []string) error {
		dests, err := collectDests(args)
		if err != nil {
			return err
		}
		return routeUp(dests)
	},
}

var routeDownCmd = &cobra.Command{
	Use:   "down [цель|пометка...]",
	Short: "Снять маршруты (sudo); без аргументов все цели",
	RunE: func(cmd *cobra.Command, args []string) error {
		dests, err := collectDests(args)
		if err != nil {
			return err
		}
		routeDown(dests)
		return nil
	},
}

var routeCleanCmd = &cobra.Command{
	Use:   "clean [--all]",
	Short: "Снять протухшие маршруты на прежний шлюз после смены сети",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := routeClean(routeAll)
		return err
	},
}

var routeListCmd = &cobra.Command{
	Use:   "list",
	Short: "Цели обхода из конфига",
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(cfg.Hosts) == 0 {
			ui.Println(ui.Dim("список пуст: tim route add <адрес|домен|подсеть>"))
		}
		for _, h := range cfg.Hosts {
			ui.Println(ui.Pad(h.Addr, 24) + " " + h.Label)
		}
		return nil
	},
}

var routeTableCmd = &cobra.Command{
	Use:   "table",
	Short: "Статические маршруты таблицы (адреса и подсети)",
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, r := range netinfo.Table() {
			if !strings.Contains(r.Flags, "S") || !strings.Contains(r.Flags, "G") {
				continue
			}
			ui.Println(ui.Pad(r.Dest, 20) + " " + ui.Pad(r.Gateway, 16) + " " + ui.Pad(r.Iface, 7) + " " + ui.Dim(cfg.HostLabel(r.Dest)))
		}
		return nil
	},
}

func expand(t netinfo.Target) []string {
	switch t.Kind {
	case netinfo.KindDomain:
		return netinfo.LookupIPv4(t.Value)
	case netinfo.KindAS:
		nets, err := asn.Prefixes(stateDir(), strings.TrimPrefix(t.Value, "as:"))
		if err != nil {
			ui.Println(ui.Warn(t.Value + ": сети не получены: " + ui.ShortErr(err)))
		}
		return nets
	}
	return []string{t.Value}
}

// stateDir: служебные файлы. Страж работает под root, и в домашней папке он наплодил бы файлов,
// которые пользователь потом не перезапишет, поэтому у root своё место.
func stateDir() string {
	if os.Geteuid() == 0 {
		if runtime.GOOS == "darwin" {
			return "/var/db/tim"
		}
		return "/var/lib/tim"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "tim")
}

// ownerTargets: домен -> автономные системы, из которых видны его адреса. Их часто несколько:
// Яндекс анонсирует одни и те же сети и из основной AS, и из дочерней. Берём все - пересечения
// склеятся при раскрытии.
func ownerTargets(domain string) ([]netinfo.Target, []string, error) {
	ips := netinfo.LookupIPv4(domain)
	if len(ips) == 0 {
		return nil, nil, ui.Errorf("%s не раскрывается в адреса", domain)
	}
	list, err := asn.Origins(ips)
	if err != nil || len(list) == 0 {
		return nil, nil, ui.Errorf("%s: владелец сети не определился: %v", domain, err)
	}
	var targets []netinfo.Target
	var names []string
	for _, a := range list {
		name := asn.Holder(a)
		if domaudit.IsCDN(name) && !routeForce {
			return nil, nil, ui.Errorf("%s сидит на CDN (%s): в обход ушла бы чужая половина интернета; если точно надо: --force", domain, name)
		}
		targets = append(targets, netinfo.Target{Kind: netinfo.KindAS, Value: "as:" + a})
		names = append(names, name)
	}
	return targets, names, nil
}

func expandAddr(addr string) []string {
	t, err := netinfo.ParseTarget(addr)
	if err != nil {
		return nil
	}
	return expand(t)
}

// collectDests: аргументы как пометки, цели из конфига или новые цели; без аргументов все цели конфига.
func collectDests(args []string) ([]string, error) {
	if len(args) == 0 {
		var all []string
		for _, h := range cfg.Hosts {
			all = append(all, expandAddr(h.Addr)...)
		}
		return all, nil
	}
	var out []string
	for _, a := range args {
		if addr, ok := cfg.ResolveHost(a); ok {
			out = append(out, expandAddr(addr)...)
			continue
		}
		t, err := netinfo.ParseTarget(a)
		if err != nil {
			return nil, err
		}
		ips := expand(t)
		if len(ips) == 0 {
			return nil, ui.Errorf("%s не раскрывается в адреса", a)
		}
		out = append(out, ips...)
	}
	return out, nil
}

// physical: активный интерфейс и его шлюз, пусто если сети нет.
func physical() (netinfo.Service, string) {
	svc, ok := netinfo.PhysIface()
	if !ok {
		return svc, ""
	}
	return svc, netinfo.IfaceGateway(svc.Device)
}

// routeState: человеческое состояние пути до адреса или подсети.
func routeState(dest string, phys netinfo.Service, gw string) (text string, bad bool) {
	var r netinfo.Route
	if strings.Contains(dest, "/") {
		for _, t := range netinfo.Table() {
			if t.Dest == dest {
				r = netinfo.Route{Gateway: t.Gateway, Iface: t.Iface}
			}
		}
		if r.Iface == "" {
			ip, _, _ := net.ParseCIDR(dest)
			r = netinfo.RouteOf(ip.String())
		}
	} else {
		r = netinfo.RouteOf(dest)
	}
	switch {
	case netinfo.IsTunnelIface(r.Iface):
		return ui.Warn("идёт в туннель " + r.Iface), true
	case gw != "" && r.Gateway != "" && r.Iface == phys.Device && r.Gateway != gw && netinfo.IsIPv4(r.Gateway):
		return ui.Bad("шлюз устарел " + r.Gateway + ", нужен tim route up"), true
	case gw != "" && r.Gateway == gw:
		return ui.Label("в обход"), false
	}
	return ui.Dim("без своего маршрута"), false
}

func routeStatus(probe bool) error {
	phys, gw := physical()
	if phys.Device == "" {
		ui.Println(ui.Warn("физический интерфейс не найден: сеть выключена?"))
	} else {
		ui.Println(ui.Dim("физический интерфейс ") + phys.Device + ui.Dim(" шлюз ") + gw)
	}
	inet := netinfo.InternetIface()
	line := ui.Dim("трафик в интернет идёт через ") + dashIfEmpty(inet)
	if netinfo.IsTunnelIface(inet) {
		line += "  " + ui.Warn("(VPN забирает весь трафик)")
	}
	ui.Println(line)
	if len(cfg.Hosts) == 0 {
		ui.Println(ui.Dim("целей обхода нет: tim route add <адрес|домен|подсеть>"))
		return nil
	}
	ui.Println(guardLine())
	ui.Println(ui.Dim(ui.Pad("ЦЕЛЬ", 24) + " " + ui.Pad("ПОМЕТКА", 11) + " " + ui.Pad("АДРЕС", 18) + " СОСТОЯНИЕ"))
	for _, h := range cfg.Hosts {
		dests := expandAddr(h.Addr)
		if len(dests) == 0 {
			ui.Println(ui.Pad(h.Addr, 24) + " " + ui.Pad(h.Label, 11) + " " + ui.Bad("не раскрывается в адреса"))
			continue
		}
		for i, d := range dests {
			name, label := h.Addr, h.Label
			if i > 0 {
				name, label = "", ""
			}
			state, _ := routeState(d, phys, gw)
			if probe && !strings.Contains(d, "/") {
				if portOpen(d, "22") {
					state += "  " + ui.Label("порт 22 отвечает")
				} else {
					state += "  " + ui.Bad("порт 22 молчит")
				}
			}
			ui.Println(ui.Pad(name, 24) + " " + ui.Pad(label, 11) + " " + ui.Pad(d, 18) + " " + state)
		}
	}
	return nil
}

func portOpen(addr, port string) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(addr, port), 2*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func kindFlag(dest string) string {
	if strings.Contains(dest, "/") {
		return "-net"
	}
	return "-host"
}

// routeUp заводит маршрут через физический шлюз для каждого адреса или подсети.
func routeUp(dests []string) error {
	phys, gw := physical()
	if phys.Device == "" || gw == "" {
		return ui.Errorf("активный физический интерфейс или его шлюз не найден")
	}
	ui.Println(ui.Dim("через ") + phys.Device + ui.Dim(" шлюз ") + gw)
	for _, d := range dests {
		if bypassed(d, phys, gw) {
			ui.Println(ui.Dim(d + ": уже в обход"))
			continue
		}
		_ = runner.Sudo("route", "-n", "delete", kindFlag(d), d)
		if err := runner.Sudo("route", "-n", "add", kindFlag(d), d, gw); err != nil {
			ui.Println(ui.Bad(d + ": завести не удалось"))
			continue
		}
		label := cfg.HostLabel(d)
		if label != "" {
			label = " " + ui.Dim(label)
		}
		ui.Println(ui.Label(d+": маршрут заведён") + label)
	}
	return nil
}

// bypassed: у адреса или подсети уже есть свой маршрут через нужный шлюз.
func bypassed(dest string, phys netinfo.Service, gw string) bool {
	if !strings.Contains(dest, "/") {
		r := netinfo.RouteOf(dest)
		return r.Gateway == gw && r.Iface == phys.Device
	}
	for _, t := range netinfo.Table() {
		if t.Dest == dest && t.Gateway == gw {
			return true
		}
	}
	return false
}

func routeDown(dests []string) {
	for _, d := range dests {
		if err := runner.Sudo("route", "-n", "delete", kindFlag(d), d); err != nil {
			ui.Println(ui.Dim(d + ": маршрута не было"))
		} else {
			ui.Println(ui.Label(d + ": маршрут снят"))
		}
	}
}

// routeClean снимает маршруты на прежний шлюз; all - по всей таблице, иначе только цели конфига.
func routeClean(all bool) (int, error) {
	_, gw := physical()
	if gw == "" {
		return 0, ui.Errorf("шлюз физического интерфейса не найден: не с чем сравнивать")
	}
	var only map[string]bool
	if !all {
		only = map[string]bool{}
		for _, h := range cfg.Hosts {
			for _, d := range expandAddr(h.Addr) {
				only[d] = true
			}
		}
	}
	stale := netinfo.Stale(netinfo.Table(), gw, only)
	if len(stale) == 0 {
		ui.Println(ui.Dim("протухших маршрутов нет"))
		return 0, nil
	}
	for _, r := range stale {
		ui.Println(ui.Warn(ui.Pad(r.Dest, 20)) + " через " + r.Gateway + " " + ui.Dim(r.Iface+", сейчас шлюз "+gw))
	}
	if !runner.Confirm("снять эти маршруты?") {
		return 0, nil
	}
	for _, r := range stale {
		if err := runner.Sudo("route", "-n", "delete", kindFlag(r.Dest), r.Dest); err != nil {
			ui.Println(ui.Bad(r.Dest + ": снять не удалось"))
		}
	}
	return len(stale), nil
}

func init() {
	routeAddCmd.Flags().StringVar(&routeLabel, "label", "", "пометка (для одной цели)")
	routeAddCmd.Flags().BoolVar(&routeNoUp, "no-up", false, "только сохранить, маршрут не заводить")
	routeAddCmd.Flags().BoolVar(&routeForce, "force", false, "разрешить частные адреса и подсети")
	routeAddCmd.Flags().BoolVar(&routeOwner, "owner", false, "для домена - все сети его владельца (AS), а не адреса домена")
	routeCleanCmd.Flags().BoolVar(&routeAll, "all", false, "по всей таблице, а не только для целей из конфига")
	routeCmd.AddCommand(routeCheckCmd, routeAddCmd, routeRmCmd, routeUpCmd, routeDownCmd, routeCleanCmd, routeListCmd, routeTableCmd)
	root.AddCommand(routeCmd)
}
