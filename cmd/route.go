package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
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

var routeAll bool

// addOpts: флаги добавления цели; у route add и vpn exclude свои экземпляры.
type addOpts struct {
	owner, noUp, force bool
	label              string
}

func (o *addOpts) bind(c *cobra.Command) {
	c.Flags().StringVar(&o.label, "label", "", "пометка (для одной цели)")
	c.Flags().BoolVar(&o.noUp, "no-up", false, "только сохранить, маршрут не заводить")
	c.Flags().BoolVar(&o.force, "force", false, "разрешить частные адреса, подсети и CDN")
	c.Flags().BoolVar(&o.owner, "owner", false, "для домена - все сети его владельца (AS), а не адреса домена")
}

var routeAddOpts addOpts

var routeAddCmd = &cobra.Command{
	Use:   "add <адрес|домен|подсеть|as:N...> [--owner] [--label x] [--no-up]",
	Short: "Добавить цель обхода в конфиг и сразу завести маршрут",
	Args:  cobra.MinimumNArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return routeAdd(args, routeAddOpts) },
}

func routeAdd(args []string, o addOpts) error {
	var dests []string
	for _, a := range args {
		t, err := netinfo.ParseTarget(a)
		if err != nil {
			return err
		}
		targets := []netinfo.Target{t}
		if o.owner && t.Kind == netinfo.KindDomain {
			domain := t.Value
			var names []string
			if targets, names, err = bypass.OwnerTargets(domain, o.force); err != nil {
				return err
			}
			if o.label == "" && len(args) == 1 {
				o.label = domain
			}
			for i, o := range targets {
				ui.Println(ui.Dim(domain + ": сети " + o.Value + " " + names[i]))
			}
		}
		for _, t := range targets {
			if netinfo.PrivateTarget(t) && !o.force {
				return ui.Errorf("%s - частная сеть, она обычно доступна только через VPN; если точно надо: --force", t.Value)
			}
			label := ""
			if len(args) == 1 {
				label = o.label
			}
			if cfg.AddHost(config.Host{Addr: t.Value, Label: label}) {
				ui.Println(ui.Label("добавлен ") + t.Value + ui.Dim(" "+label))
			} else {
				ui.Println(ui.Dim(t.Value + ": уже в списке"))
			}
			dests = append(dests, bypass.Expand(t)...)
		}
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if o.noUp {
		return nil
	}
	return bypass.Up(cfg, dests)
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
				dests = append(dests, bypass.ExpandAddr(h.Addr)...)
				ui.Println(ui.Label("убран ") + h.Addr)
			}
		}
		if err := saveConfig(cfg); err != nil {
			return err
		}
		bypass.Down(dests)
		return nil
	},
}

var routeUpCmd = &cobra.Command{
	Use:   "up [цель|пометка...]",
	Short: "Завести маршруты через физический шлюз (sudo); без аргументов все цели",
	RunE: func(cmd *cobra.Command, args []string) error {
		dests, err := bypass.CollectDests(cfg, args)
		if err != nil {
			return err
		}
		return bypass.Up(cfg, dests)
	},
}

var routeDownCmd = &cobra.Command{
	Use:   "down [цель|пометка...]",
	Short: "Снять маршруты (sudo); без аргументов все цели",
	RunE: func(cmd *cobra.Command, args []string) error {
		dests, err := bypass.CollectDests(cfg, args)
		if err != nil {
			return err
		}
		bypass.Down(dests)
		return nil
	},
}

var routeCleanCmd = &cobra.Command{
	Use:   "clean [--all]",
	Short: "Снять протухшие маршруты на прежний шлюз после смены сети",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return bypass.Clean(cfg, routeAll)
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

func routeStatus(probe bool) error {
	phys, gw := bypass.Physical()
	if phys.Device == "" {
		ui.Println(ui.Warn("физический интерфейс не найден: сеть выключена?"))
	} else {
		ui.Println(ui.Dim("физический интерфейс ") + phys.Device + ui.Dim(" шлюз ") + gw)
	}
	inet := netinfo.InternetIface()
	line := ui.Dim("трафик в интернет идёт через ") + ui.Dash(inet)
	if netinfo.IsTunnelIface(inet) {
		line += "  " + ui.Warn("(VPN забирает весь трафик)")
	}
	ui.Println(line)
	if len(cfg.Hosts) == 0 {
		ui.Println(ui.Dim("целей обхода нет: tim route add <адрес|домен|подсеть>"))
		return nil
	}
	ui.Println(bypass.GuardLine())
	ui.Println(ui.Dim(ui.Pad("ЦЕЛЬ", 24) + " " + ui.Pad("ПОМЕТКА", 11) + " " + ui.Pad("АДРЕС", 18) + " СОСТОЯНИЕ"))
	for _, h := range cfg.Hosts {
		dests := bypass.ExpandAddr(h.Addr)
		if len(dests) == 0 {
			ui.Println(ui.Pad(h.Addr, 24) + " " + ui.Pad(h.Label, 11) + " " + ui.Bad("не раскрывается в адреса"))
			continue
		}
		for i, d := range dests {
			name, label := h.Addr, h.Label
			if i > 0 {
				name, label = "", ""
			}
			state, _ := bypass.RouteState(d, phys, gw)
			if probe && !strings.Contains(d, "/") {
				if bypass.PortOpen(d, "22") {
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

func init() {
	routeAddOpts.bind(routeAddCmd)
	routeCleanCmd.Flags().BoolVar(&routeAll, "all", false, "по всей таблице, а не только для целей из конфига")
	routeCmd.AddCommand(routeCheckCmd, routeAddCmd, routeRmCmd, routeUpCmd, routeDownCmd, routeCleanCmd, routeListCmd, routeTableCmd)
	root.AddCommand(routeCmd)
}
