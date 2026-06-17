package bypass

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/privileged"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	tableMu    sync.Mutex
	tableCache []netinfo.TableRoute
)

// fewDests: до стольких целей печатается строка на каждую, дальше - ход и итог.
const fewDests = 5

// Physical: активный интерфейс и его шлюз, пусто если сети нет.
func Physical() (netinfo.Service, string) {
	svc, ok := netinfo.PhysIface()
	if !ok {
		return svc, ""
	}
	return svc, netinfo.IfaceGateway(svc.Device)
}

// table: таблица маршрутов, прочитанная один раз на команду, а не на каждую подсеть;
// TableChanged сбрасывает её после правки маршрутов.
func table() []netinfo.TableRoute {
	tableMu.Lock()
	defer tableMu.Unlock()
	if tableCache == nil {
		tableCache = netinfo.Table()
	}
	return tableCache
}

func TableChanged() {
	tableMu.Lock()
	tableCache = nil
	tableMu.Unlock()
}

// RouteState: человеческое состояние пути до адреса или подсети.
func RouteState(dest string, phys netinfo.Service, gw string) (text string, bad bool) {
	var r netinfo.Route
	if strings.Contains(dest, "/") {
		for _, t := range table() {
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

func PortOpen(addr, port string) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(addr, port), 2*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// setRoute ставит маршрут через шлюз. macOS не меняет существующий маршрут командой add,
// поэтому старый снимается первым; ip route replace на Linux делает это сам.
func setRoute(dest, gw, iface string) error {
	if runtime.GOOS != "linux" {
		// Старого маршрута обычно нет, и route ругается "not in table" - это не ошибка.
		_, _ = privileged.Run(netinfo.DelRouteCmd(dest)...)
	}
	defer TableChanged()
	_, err := privileged.Run(netinfo.AddRouteCmd(dest, gw, iface)...)
	return err
}

// Up заводит маршрут через физический шлюз для каждого адреса или подсети.
func Up(cfg *config.Config, dests []string) error {
	phys, gw := Physical()
	if phys.Device == "" || gw == "" {
		return ui.Errorf("активный физический интерфейс или его шлюз не найден")
	}
	ui.Println(ui.Dim("через ") + phys.Device + ui.Dim(" шлюз ") + gw)
	verbose := len(dests) <= fewDests || runner.DryRun
	done, had := 0, 0
	var failed []string
	for i, d := range dests {
		if Bypassed(d, phys, gw) {
			had++
			if verbose {
				ui.Println(ui.Dim(d + ": уже в обход"))
			}
			continue
		}
		if !verbose && ui.Terminal() {
			fmt.Fprintf(os.Stderr, "\r\033[2K"+ui.Dim("маршруты: %d из %d"), i+1, len(dests))
		}
		if err := setRoute(d, gw, phys.Device); err != nil {
			failed = append(failed, d+": "+err.Error())
			if verbose {
				ui.Println(ui.Bad(d + ": завести не удалось: " + err.Error()))
			}
			continue
		}
		done++
		if verbose {
			label := cfg.HostLabel(d)
			if label != "" {
				label = " " + ui.Dim(label)
			}
			ui.Println(ui.Label(d+": маршрут заведён") + label)
		}
	}
	if verbose {
		return upError(failed, len(dests))
	}
	if ui.Terminal() {
		fmt.Fprint(os.Stderr, "\r\033[2K")
	}
	line := ui.Label(fmt.Sprintf("заведено %d", done))
	if had > 0 {
		line += ui.Dim(fmt.Sprintf(", уже были %d", had))
	}
	if len(failed) > 0 {
		line += "  " + ui.Bad(fmt.Sprintf("не удалось %d", len(failed)))
	}
	ui.Println(line)
	for _, f := range failed[:min(fewDests, len(failed))] {
		ui.Println(ui.Bad("  " + f))
	}
	return upError(failed, len(dests))
}

func upError(failed []string, total int) error {
	if len(failed) == 0 {
		return nil
	}
	return ui.Errorf("не завелось маршрутов: %d из %d", len(failed), total)
}

// Bypassed: у адреса или подсети уже есть свой маршрут через нужный шлюз.
func Bypassed(dest string, phys netinfo.Service, gw string) bool {
	if !strings.Contains(dest, "/") {
		r := netinfo.RouteOf(dest)
		return r.Gateway == gw && r.Iface == phys.Device
	}
	for _, t := range table() {
		if t.Dest == dest && t.Gateway == gw {
			return true
		}
	}
	return false
}

// Down снимает маршруты. Маршрута не было - не ошибка; отказ системы или sudo - ошибка.
func Down(dests []string) error {
	verbose := len(dests) <= fewDests || runner.DryRun
	gone := 0
	var failed []string
	for _, d := range dests {
		TableChanged()
		out, err := privileged.Run(netinfo.DelRouteCmd(d)...)
		switch {
		case err == nil:
			gone++
			if verbose {
				ui.Println(ui.Label(d + ": маршрут снят"))
			}
		case routeMissing(out + " " + err.Error()):
			if verbose {
				ui.Println(ui.Dim(d + ": маршрута не было"))
			}
		default:
			failed = append(failed, d+": "+firstLine(out, err))
			if verbose {
				ui.Println(ui.Bad(d + ": снять не удалось: " + firstLine(out, err)))
			}
		}
	}
	if !verbose {
		ui.Println(ui.Label(fmt.Sprintf("снято %d", gone)) + ui.Dim(fmt.Sprintf(" из %d", len(dests))))
		for _, f := range failed[:min(fewDests, len(failed))] {
			ui.Println(ui.Bad("  " + f))
		}
	}
	if len(failed) > 0 {
		return ui.Errorf("не снялось маршрутов: %d из %d", len(failed), len(dests))
	}
	return nil
}

// routeMissing: route (macOS) и ip (Linux) так отвечают, когда снимать нечего.
func routeMissing(msg string) bool {
	return strings.Contains(msg, "not in table") || strings.Contains(msg, "No such process")
}

// Clean снимает маршруты на прежний шлюз; all - по всей таблице, иначе только цели конфига.
func Clean(cfg *config.Config, all bool) error {
	_, gw := Physical()
	if gw == "" {
		return ui.Errorf("шлюз физического интерфейса не найден: не с чем сравнивать")
	}
	var only map[string]bool
	if !all {
		only = map[string]bool{}
		for _, h := range cfg.Hosts {
			for _, d := range ExpandAddr(h.Addr) {
				only[d] = true
			}
		}
	}
	stale := netinfo.Stale(netinfo.Table(), gw, only)
	if len(stale) == 0 {
		ui.Println(ui.Dim("протухших маршрутов нет"))
		return nil
	}
	for _, r := range stale {
		ui.Println(ui.Warn(ui.Pad(r.Dest, 20)) + " через " + r.Gateway + " " + ui.Dim(r.Iface+", сейчас шлюз "+gw))
	}
	if !runner.Confirm("снять эти маршруты?") {
		return nil
	}
	gone := 0
	for _, r := range stale {
		TableChanged()
		if _, err := privileged.Run(netinfo.DelRouteCmd(r.Dest)...); err != nil {
			ui.Println(ui.Bad(r.Dest + ": снять не удалось: " + err.Error()))
			continue
		}
		gone++
	}
	ui.Println(ui.Label(fmt.Sprintf("снято %d", gone)))
	return nil
}
