package bypass

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/privileged"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// macOS: pf заворачивает TCP группы novpn на lo0 и дальше на прозрачный прокси tim, а тот ходит
// к адресату сокетом, привязанным к физическому интерфейсу. Подменить источник пакета после
// route-to ядро не даёт, ответ на адрес туннеля ушёл бы в сам туннель - поэтому прокси и reply-to.

const (
	Anchor    = "com.apple/tim" // внутри com.apple/*, который pf.conf macOS подключает сам
	ProxyPort = 7788
)

func ProxyPidFile() string { return filepath.Join(GuardDir(), "novpn-proxy.pid") }
func ProxyLogFile() string { return "/var/log/tim-novpn.log" }

// ProxyPID: pid живого прокси или 0.
func ProxyPID() int {
	data, err := os.ReadFile(ProxyPidFile())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 || !alive(pid) {
		return 0
	}
	return pid
}

// darwinGID: группа через dscl; gid - первый свободный от 700.
func darwinGID() (int, error) {
	if out, err := runner.Output("dscl", ".", "-read", "/Groups/"+GroupName, "PrimaryGroupID"); err == nil {
		if _, v, ok := strings.Cut(strings.TrimSpace(out), ":"); ok {
			if gid, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				return gid, nil
			}
		}
	}
	used := map[int]bool{}
	out, _ := runner.Output("dscl", ".", "-list", "/Groups", "PrimaryGroupID")
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			if n, err := strconv.Atoi(f[1]); err == nil {
				used[n] = true
			}
		}
	}
	gid := 700
	for used[gid] {
		gid++
	}
	if _, err := privileged.Run("dscl", ".", "-create", "/Groups/"+GroupName, "PrimaryGroupID", strconv.Itoa(gid)); err != nil {
		return 0, ui.Errorf("группа %s не завелась: %v", GroupName, err)
	}
	_, _ = privileged.Run("dscl", ".", "-create", "/Groups/"+GroupName, "RealName", "tim: мимо VPN")
	ui.Println(ui.Label("группа "+GroupName) + ui.Dim("  gid "+strconv.Itoa(gid)))
	return gid, nil
}

// Rules: правила pf. Локальные адреса не трогаем, иначе под раздачу попали бы dev-серверы на 127.0.0.1.
func Rules(tunnels []netinfo.Tunnel) string {
	var b strings.Builder
	b.WriteString("# tim: процессы группы " + GroupName + " идут мимо VPN через прокси tim\n")
	b.WriteString("table <tim_local> { 127.0.0.0/8, " + strings.Join(netinfo.PrivateNets, ", ") + ", 169.254.0.0/16 }\n")
	fmt.Fprintf(&b, "rdr on lo0 inet proto tcp from any to ! <tim_local> -> 127.0.0.1 port %d\n", ProxyPort)
	// Ответ прокси идёт на адрес туннеля, а маршрут к нему у utun ведёт в сам туннель: reply-to
	// возвращает ответ на lo0, где он доходит до приложения.
	fmt.Fprintf(&b, "pass in quick on lo0 reply-to (lo0 127.0.0.1) inet proto tcp from any to 127.0.0.1 port %d keep state\n", ProxyPort)
	for _, t := range tunnels {
		// Следующий узел для lo0 задаётся явно, как у Clash и Surge: без него pf пакет не заворачивает.
		fmt.Fprintf(&b, "pass out quick on %s route-to (lo0 127.0.0.1) inet proto tcp from any to ! <tim_local> group %s keep state\n", t.Iface, GroupName)
	}
	return b.String()
}

// ProxyUp: прокси под root живёт отдельно от tim; если уже работает - ничего не делаем.
func ProxyUp(phys netinfo.Service) error {
	if ProxyPID() > 0 {
		return nil
	}
	ip := netinfo.IfaceIPv4(phys.Device)
	if ip == "" {
		return ui.Errorf("у %s нет IPv4-адреса", phys.Device)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	resp, err := privileged.Launch(privileged.LaunchReq{Path: exe, Args: []string{"novpn-proxy", "--iface", phys.Device, "--ip", ip, "--port", strconv.Itoa(ProxyPort)},
		Env: []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=/var/root"}, Dir: "/"})
	if err != nil {
		return err
	}
	if resp.Err != "" {
		return ui.Errorf("прокси не запустился: %s", resp.Err)
	}
	if runner.DryRun {
		return nil
	}
	for i := 0; i < 20; i++ {
		if ProxyPID() > 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ui.Errorf("прокси не поднялся, смотри %s", ProxyLogFile())
}

// pfOn: прокси, правила, ip forwarding, scoped-маршрут, включить pf.
func pfOn(phys netinfo.Service, gw string, tunnels []netinfo.Tunnel) error {
	if err := ProxyUp(phys); err != nil {
		return err
	}
	rules := Rules(tunnels)
	if runner.DryRun {
		ui.Println(ui.Dim(strings.TrimRight(rules, "\n")))
	}
	if out, err := privileged.RunStdin(rules, "pfctl", "-q", "-a", Anchor, "-f", "-"); err != nil {
		return ui.Errorf("pf не принял правила: %s", firstLine(out, err))
	}
	if err := forwardingOn(); err != nil {
		return err
	}
	if err := scopedDefaultOn(phys, gw); err != nil {
		return err
	}
	if out, err := privileged.Run("pfctl", "-q", "-e"); err != nil && !strings.Contains(out+err.Error(), "already enabled") {
		return ui.Errorf("pf не включился: %s", firstLine(out, err))
	}
	ui.Println(ui.Label("мимо VPN") + ui.Dim("  группа "+GroupName+", прокси 127.0.0.1:"+strconv.Itoa(ProxyPort)+" через "+phys.Device+" "+netinfo.IfaceIPv4(phys.Device)))
	return nil
}

// forwardingOn: без ip forwarding ядро выбрасывает завёрнутый на lo0 пакет с чужим адресатом
// раньше, чем rdr успевает подменить адрес, - прокси его не увидит. Тот же шаг у Clash и Surge.
func forwardingOn() error {
	cur, _ := runner.Output("sysctl", "-n", "net.inet.ip.forwarding")
	if strings.TrimSpace(cur) == "1" {
		return nil
	}
	if _, err := privileged.Run("sysctl", "-w", "net.inet.ip.forwarding=1"); err != nil {
		return ui.Errorf("ip forwarding не включился: %v", err)
	}
	remember("forwarding", "0")
	ui.Println(ui.Dim("включён net.inet.ip.forwarding - без него pf не довозит пакет до прокси; --off вернёт 0"))
	return nil
}

// forwardingRestore возвращает forwarding, если его включал tim; памятка стирается только после удачи.
func forwardingRestore() error {
	prev, ok := recall("forwarding")
	if !ok {
		return nil
	}
	if out, err := privileged.Run("sysctl", "-w", "net.inet.ip.forwarding="+prev); err != nil {
		return ui.Errorf("ip forwarding не вернулся в %s: %s", prev, firstLine(out, err))
	}
	forget("forwarding")
	return nil
}

// scopedDefaultOn: сокет прокси привязан к en0 (IP_BOUND_IF), и маршрут для него ядро ищет только
// среди scoped-маршрутов этого интерфейса. VPN-клиент, забирая маршрут по умолчанию, обычно сносит
// и scoped-копию - тогда "no route to host". Заводим её заново; глобальную таблицу она не трогает.
func scopedDefaultOn(phys netinfo.Service, gw string) error {
	if scopedGateway(phys.Device) == gw {
		return nil
	}
	if _, err := privileged.Run("route", "-n", "add", "-ifscope", phys.Device, "default", gw); err != nil && !strings.Contains(err.Error(), "exists") {
		return ui.Errorf("scoped-маршрут для %s не завёлся: %v", phys.Device, err)
	}
	remember("scoped", phys.Device)
	ui.Println(ui.Dim("заведён scoped-маршрут по умолчанию для " + phys.Device + " через " + gw + " - по нему ходит прокси; --off снимет"))
	return nil
}

func scopedGateway(dev string) string {
	out, _ := runner.Output("route", "-n", "get", "-ifscope", dev, "1.1.1.1")
	for _, l := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), ":"); ok && k == "gateway" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func scopedRestore() error {
	dev, ok := recall("scoped")
	if !ok {
		return nil
	}
	out, err := privileged.Run("route", "-n", "delete", "-ifscope", dev, "default")
	if err != nil && !strings.Contains(out+err.Error(), "not in table") {
		return ui.Errorf("scoped-маршрут %s не снялся: %s", dev, firstLine(out, err))
	}
	forget("scoped")
	return nil
}

// pfOff снимает всё, что снимается, и возвращает то, что снять не вышло.
func pfOff() error {
	var errs []error
	if out, err := privileged.Run("pfctl", "-q", "-a", Anchor, "-F", "all"); err != nil {
		errs = append(errs, ui.Errorf("правила pf не снялись: %s", firstLine(out, err)))
	}
	if pid := ProxyPID(); pid > 0 {
		if out, err := privileged.Run("kill", strconv.Itoa(pid)); err != nil {
			errs = append(errs, ui.Errorf("прокси (pid %d) не остановился: %s", pid, firstLine(out, err)))
		}
	}
	errs = append(errs, forwardingRestore(), scopedRestore())
	if err := errors.Join(errs...); err != nil {
		return err
	}
	ui.Println(ui.Label("снято") + ui.Dim("  правила pf, прокси, forwarding и scoped-маршрут; программы под группой снова пойдут через VPN"))
	return nil
}

// pfStatus: pf, прокси, правила анкора, состояния заворота, forwarding и scoped-маршрут.
func pfStatus() {
	// Без -q: с ним pfctl молчит и про правила, и про состояния.
	if info, err := privileged.Run("pfctl", "-s", "info"); err == nil {
		for _, l := range strings.Split(info, "\n") {
			if strings.HasPrefix(l, "Status:") {
				ui.Println(ui.Label("pf ") + strings.TrimSpace(strings.TrimPrefix(l, "Status:")))
			}
		}
	}
	if pid := ProxyPID(); pid > 0 {
		ui.Println(ui.Label("прокси ") + "работает, pid " + strconv.Itoa(pid) + ui.Dim("  журнал "+ProxyLogFile()))
	} else {
		ui.Println(ui.Dim("прокси не запущен"))
	}
	rules, err := privileged.Run("pfctl", "-a", Anchor, "-s", "all")
	switch {
	case err != nil:
		ui.Println(ui.Dim("правил pf нет: " + firstLine(rules, err)))
	case strings.TrimSpace(rules) == "":
		ui.Println(ui.Dim("правил pf нет: tim direct <приложение> или --check"))
	default:
		ui.Println(ui.Label("правила pf в " + Anchor))
		for _, l := range strings.Split(rules, "\n") {
			if strings.HasPrefix(l, "nat ") || strings.HasPrefix(l, "rdr ") || strings.HasPrefix(l, "pass ") || strings.HasPrefix(l, "block ") {
				ui.Println("  " + ui.Dim(l))
			}
		}
	}
	var tunIPs []string
	for _, t := range netinfo.Tunnels() {
		tunIPs = append(tunIPs, t.IPv4...)
	}
	phys, _ := Physical()
	if states, err := privileged.Run("pfctl", "-s", "states"); err == nil {
		redir, tun := 0, 0
		var sample []string
		for _, l := range strings.Split(states, "\n") {
			fromTunnel := false
			for _, ip := range tunIPs {
				fromTunnel = fromTunnel || strings.Contains(l, ip+":")
			}
			switch {
			case fromTunnel && strings.Contains(l, "127.0.0.1:"+strconv.Itoa(ProxyPort)):
				redir++
				if len(sample) < 3 {
					sample = append(sample, strings.TrimSpace(l))
				}
			case fromTunnel:
				tun++
			}
		}
		ui.Println(ui.Label("состояния: ") + fmt.Sprintf("завёрнуто на прокси %d, в туннеле %d", redir, tun))
		for _, l := range sample {
			ui.Println("  " + ui.Dim(l))
		}
		if redir > 0 {
			ui.Println(ui.OK("правило срабатывает: соединения группы идут через прокси мимо VPN"))
		}
	}
	fw, _ := runner.Output("sysctl", "-n", "net.inet.ip.forwarding")
	ui.Println(ui.Label("ip forwarding ") + ui.Dash(strings.TrimSpace(fw)))
	ui.Println(ui.Label("scoped-маршрут "+phys.Device+" ") + ui.Dash(scopedGateway(phys.Device)))
}

// pfCaptureStart: пока идёт проверка, tcpdump под root пишет, что видно на lo0 и в туннеле; по
// этому понятно, где застрял пакет. Возвращает функцию, которая при неудаче печатает дамп.
func pfCaptureStart() func(show bool) {
	if runtime.GOOS != "darwin" || runner.DryRun {
		return func(bool) {}
	}
	pcap := filepath.Join(GuardDir(), "novpn-check.pcap")
	started := time.Now()
	ifaces := "pktap,lo0"
	for _, t := range netinfo.Tunnels() {
		ifaces += "," + t.Iface
	}
	filter := "tcp and port " + strconv.Itoa(ProxyPort)
	if ips, err := net.LookupIP("ifconfig.co"); err == nil {
		for _, ip := range ips {
			if ip.To4() != nil {
				filter = "tcp and (port " + strconv.Itoa(ProxyPort) + " or host " + ip.String() + ")"
				break
			}
		}
	}
	r, err := privileged.Launch(privileged.LaunchReq{Path: "/usr/sbin/tcpdump", Args: []string{"-i", ifaces, "-n", "-G", "6", "-W", "1", "-w", pcap, filter},
		Env: []string{"PATH=/usr/sbin:/usr/bin:/bin"}, Dir: "/"})
	if err != nil || r.Err != "" {
		return func(bool) {}
	}
	time.Sleep(800 * time.Millisecond)
	return func(show bool) {
		if !show {
			return
		}
		if wait := 7*time.Second - time.Since(started); wait > 0 {
			time.Sleep(wait)
		}
		out, err := runner.Output("tcpdump", "-n", "-k", "-r", pcap)
		if err != nil {
			return
		}
		ui.Println(ui.Label("пакеты за время проверки ") + ui.Dim("(lo0 и туннель, порт прокси и ifconfig.co)"))
		lines := strings.Split(strings.TrimSpace(out), "\n")
		for i, l := range lines {
			if i >= 16 {
				ui.Println(ui.Dim(fmt.Sprintf("  ещё %d", len(lines)-16)))
				break
			}
			ui.Println("  " + ui.Dim(l))
		}
		if strings.TrimSpace(out) == "" {
			ui.Println(ui.Dim("  ничего: пакеты до lo0 не дошли"))
		}
	}
}
