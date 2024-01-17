package cmd

import (
	"net"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var routeCmd = &cobra.Command{
	Use:   "route",
	Short: "Маршруты в обход VPN для адресов из конфига",
	RunE:  func(cmd *cobra.Command, args []string) error { return routeStatus(false) },
}

var routeCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Состояние плюс проверка порта 22",
	RunE:  func(cmd *cobra.Command, args []string) error { return routeStatus(true) },
}

var routeUpCmd = &cobra.Command{
	Use:   "up [адрес|пометка...]",
	Short: "Завести host-маршруты через физический шлюз (sudo)",
	RunE: func(cmd *cobra.Command, args []string) error {
		addrs, err := collectAddrs(args)
		if err != nil {
			return err
		}
		return routeUp(addrs)
	},
}

var routeDownCmd = &cobra.Command{
	Use:   "down [адрес|пометка...]",
	Short: "Снять host-маршруты (sudo)",
	RunE: func(cmd *cobra.Command, args []string) error {
		addrs, err := collectAddrs(args)
		if err != nil {
			return err
		}
		for _, a := range addrs {
			if err := runner.Sudo("route", "-n", "delete", "-host", a); err != nil {
				ui.Println(ui.Dim(a + ": маршрута не было"))
			} else {
				ui.Println(ui.Label(a + ": маршрут снят"))
			}
		}
		return nil
	},
}

var routeListCmd = &cobra.Command{
	Use:   "list",
	Short: "Адреса из конфига",
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, h := range cfg.Hosts {
			ui.Println(ui.Pad(h.Addr, 16) + " " + h.Label)
		}
		return nil
	},
}

var routeTableCmd = &cobra.Command{
	Use:   "table",
	Short: "Все host-маршруты таблицы",
	RunE: func(cmd *cobra.Command, args []string) error {
		for _, r := range netinfo.HostRoutes() {
			ui.Println(ui.Pad(r.Dest, 16) + " " + ui.Pad(r.Gateway, 16) + " " + r.Iface + "  " + ui.Dim(cfg.HostLabel(r.Dest)))
		}
		return nil
	},
}

// physical: активный интерфейс и его шлюз, пусто если сети нет.
func physical() (netinfo.Service, string) {
	svc, ok := netinfo.PhysIface()
	if !ok {
		return svc, ""
	}
	return svc, netinfo.IfaceGateway(svc.Device)
}

// routeState: человеческое состояние маршрута до адреса.
func routeState(addr string, phys netinfo.Service, gw string) (text string, bad bool) {
	r := netinfo.RouteOf(addr)
	switch {
	case netinfo.IsTunnelIface(r.Iface):
		return ui.Warn("идёт в туннель " + r.Iface), true
	case gw != "" && r.Gateway != "" && r.Iface == phys.Device && r.Gateway != gw:
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
	def := netinfo.DefaultIface()
	line := ui.Dim("маршрут по умолчанию ") + def
	if netinfo.IsTunnelIface(def) {
		line += "  " + ui.Warn("(весь трафик в туннель)")
	}
	ui.Println(line)
	ui.Println(ui.Dim(ui.Pad("АДРЕС", 16) + " " + ui.Pad("ПОМЕТКА", 11) + " " + ui.Pad("ШЛЮЗ", 16) + " " + ui.Pad("IFACE", 6) + " СОСТОЯНИЕ"))
	for _, h := range cfg.Hosts {
		r := netinfo.RouteOf(h.Addr)
		state, _ := routeState(h.Addr, phys, gw)
		if probe {
			if portOpen(h.Addr, "22") {
				state += "  " + ui.Label("порт 22 отвечает")
			} else {
				state += "  " + ui.Bad("порт 22 молчит")
			}
		}
		ui.Println(ui.Pad(h.Addr, 16) + " " + ui.Pad(h.Label, 11) + " " + ui.Pad(dashIfEmpty(r.Gateway), 16) + " " + ui.Pad(dashIfEmpty(r.Iface), 6) + " " + state)
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

// collectAddrs: аргументы как адреса, пометки или имена; без аргументов весь список.
func collectAddrs(args []string) ([]string, error) {
	if len(args) == 0 {
		var all []string
		for _, h := range cfg.Hosts {
			all = append(all, h.Addr)
		}
		return all, nil
	}
	var out []string
	for _, a := range args {
		if addr, ok := cfg.ResolveHost(a); ok {
			out = append(out, addr)
			continue
		}
		if ip := netinfo.ResolveIPv4(a); ip != "" {
			out = append(out, ip)
			continue
		}
		return nil, ui.Errorf("не удалось разобрать адрес: %s", a)
	}
	return out, nil
}

// routeUp заводит host-маршрут через физический шлюз для каждого адреса.
func routeUp(addrs []string) error {
	phys, gw := physical()
	if phys.Device == "" || gw == "" {
		return ui.Errorf("активный физический интерфейс или его шлюз не найден")
	}
	ui.Println(ui.Dim("через ") + phys.Device + ui.Dim(" шлюз ") + gw)
	for _, a := range addrs {
		r := netinfo.RouteOf(a)
		if r.Gateway == gw && r.Iface == phys.Device {
			ui.Println(ui.Dim(a + ": уже в обход"))
			continue
		}
		_ = runner.Sudo("route", "-n", "delete", "-host", a)
		if err := runner.Sudo("route", "-n", "add", "-host", a, gw); err != nil {
			ui.Println(ui.Bad(a + ": завести не удалось"))
			continue
		}
		label := cfg.HostLabel(a)
		if label != "" {
			label = " " + ui.Dim(label)
		}
		ui.Println(ui.Label(a+": маршрут заведён") + label)
	}
	return nil
}

func init() {
	routeCmd.AddCommand(routeCheckCmd, routeUpCmd, routeDownCmd, routeListCmd, routeTableCmd)
	root.AddCommand(routeCmd)
}
