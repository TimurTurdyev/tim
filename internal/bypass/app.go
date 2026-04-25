package bypass

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/privileged"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Обход по приложению: процессы группы novpn pf заворачивает на прозрачный прокси tim, тот ходит
// к адресату через физический интерфейс. Маршруты приложений не различают, pf - различает по группе.

const (
	GroupName = "novpn"
	Anchor    = "com.apple/tim" // внутри com.apple/*, который pf.conf macOS подключает сам
)

const ProxyPort = 7788

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

// GID: группа novpn, при первом разе заводится.
func GID() (int, error) {
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

// Rules: правила pf. Соединения процессов группы заворачиваются на lo0 и дальше (rdr) на прокси
// tim, который сам ходит к адресату через физический интерфейс. Локальные адреса не трогаем,
// иначе под раздачу попали бы dev-серверы на 127.0.0.1.
func Rules(tunnels []netinfo.Tunnel) string {
	var b strings.Builder
	b.WriteString("# tim: процессы группы " + GroupName + " идут мимо VPN через прокси tim\n")
	b.WriteString("table <tim_local> { 127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16 }\n")
	// Источник подменяется на 127.0.0.1: ответ на адрес туннеля ядро отправило бы в сам туннель
	// (маршрут к своему адресу у utun ведёт в него же), а с check_interface не приняло бы и с en0.
	for _, t := range tunnels {
		for _, ip := range t.IPv4 {
			fmt.Fprintf(&b, "nat on lo0 inet proto tcp from %s to ! <tim_local> -> 127.0.0.1\n", ip)
		}
	}
	fmt.Fprintf(&b, "rdr pass on lo0 inet proto tcp from any to ! <tim_local> -> 127.0.0.1 port %d\n", ProxyPort)
	for _, t := range tunnels {
		// Следующий узел для lo0 задаётся явно, как у Clash и Surge: без него pf пакет не заворачивает.
		fmt.Fprintf(&b, "pass out quick on %s route-to (lo0 127.0.0.1) inet proto tcp from any to ! <tim_local> group %s keep state\n", t.Iface, GroupName)
	}
	return b.String()
}

// IfaceIPv4: адрес интерфейса - источник для исходящих соединений прокси.
func IfaceIPv4(dev string) string {
	ifi, err := net.InterfaceByName(dev)
	if err != nil {
		return ""
	}
	addrs, _ := ifi.Addrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.To4().String()
		}
	}
	return ""
}

// ProxyUp: прокси под root живёт отдельно от tim; если уже работает - ничего не делаем.
func ProxyUp(phys netinfo.Service) error {
	if ProxyPID() > 0 {
		return nil
	}
	ip := IfaceIPv4(phys.Device)
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

// AppOn заводит группу, прокси и правила; возвращает gid.
func AppOn() (int, error) {
	phys, gw := Physical()
	if phys.Device == "" || gw == "" {
		return 0, ui.Errorf("физический интерфейс или шлюз не найден: сети нет?")
	}
	tunnels := netinfo.Tunnels()
	if len(tunnels) == 0 {
		return 0, ui.Errorf("туннеля нет: VPN выключен, приложения и так идут напрямую")
	}
	gid, err := GID()
	if err != nil {
		return 0, err
	}
	if err := ProxyUp(phys); err != nil {
		return 0, err
	}
	rules := Rules(tunnels)
	if runner.DryRun {
		ui.Println(ui.Dim(strings.TrimRight(rules, "\n")))
	}
	if out, err := privileged.RunStdin(rules, "pfctl", "-q", "-a", Anchor, "-f", "-"); err != nil {
		return 0, ui.Errorf("pf не принял правила: %s", firstLine(out, err))
	}
	if out, err := privileged.Run("pfctl", "-q", "-e"); err != nil && !strings.Contains(out+err.Error(), "already enabled") {
		return 0, ui.Errorf("pf не включился: %s", firstLine(out, err))
	}
	ui.Println(ui.Label("мимо VPN") + ui.Dim("  группа "+GroupName+", прокси 127.0.0.1:"+strconv.Itoa(ProxyPort)+" через "+phys.Device+" "+IfaceIPv4(phys.Device)))
	return gid, nil
}

func firstLine(out string, err error) string {
	if out == "" && err != nil {
		return err.Error()
	}
	return strings.SplitN(out, "\n", 2)[0]
}

func AppOff() error {
	if _, err := privileged.Run("pfctl", "-q", "-a", Anchor, "-F", "all"); err != nil {
		return ui.Errorf("снять правила не вышло: %v", err)
	}
	if pid := ProxyPID(); pid > 0 {
		_, _ = privileged.Run("kill", strconv.Itoa(pid))
	}
	ui.Println(ui.Label("снято") + ui.Dim("  правила pf и прокси; запущенные под группой программы снова пойдут через VPN"))
	return nil
}

// AppStatus: включён ли pf, какие правила стоят, кто сейчас работает под группой.
func AppStatus() error {
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
		ui.Println(ui.Dim("правил pf нет: tim vpn app <приложение> или --check"))
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
	if states, err := privileged.Run("pfctl", "-s", "states"); err == nil {
		redir, tun := 0, 0
		for _, l := range strings.Split(states, "\n") {
			fromTunnel := false
			for _, ip := range tunIPs {
				fromTunnel = fromTunnel || strings.Contains(l, ip+":")
			}
			switch {
			case fromTunnel && strings.Contains(l, "127.0.0.1:"+strconv.Itoa(ProxyPort)):
				redir++
			case fromTunnel:
				tun++
			}
		}
		ui.Println(ui.Label("состояния: ") + fmt.Sprintf("завёрнуто на прокси %d, в туннеле %d", redir, tun))
		if redir > 0 {
			ui.Println(ui.OK("правило срабатывает: соединения группы идут через прокси мимо VPN"))
		}
	}
	gid, err := strconv.Atoi(strings.TrimSpace(func() string {
		o, _ := runner.Output("dscl", ".", "-read", "/Groups/"+GroupName, "PrimaryGroupID")
		_, v, _ := strings.Cut(strings.TrimSpace(o), ":")
		return v
	}()))
	if err != nil {
		ui.Println(ui.Dim("группы " + GroupName + " нет"))
		return nil
	}
	ps, _ := runner.Output("ps", "-axo", "pid=,gid=,comm=")
	var mine []string
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[1] == strconv.Itoa(gid) {
			mine = append(mine, procs.AppName(f[2]))
		}
	}
	if len(mine) == 0 {
		ui.Println(ui.Dim("под группой " + GroupName + " сейчас никто не работает"))
		return nil
	}
	ui.Println(ui.Label("мимо VPN сейчас ") + strings.Join(Uniq(mine), ", "))
	return nil
}

// AppCheck: правило на месте и действительно работает - curl под группой видит другую страну.
func AppCheck() error {
	gid, err := AppOn()
	if err != nil {
		return err
	}
	viaVPN := strings.TrimSpace(func() string {
		o, _ := runner.Output("curl", "-s", "--max-time", "8", "https://ifconfig.co/country")
		return o
	}())
	resp, err := privileged.Launch(privileged.LaunchReq{Path: "/usr/bin/curl", Args: []string{"-s", "--max-time", "8", "https://ifconfig.co/country"},
		Env: os.Environ(), Dir: paths.Home(), UID: os.Getuid(), GID: gid, Groups: myGroups(), Wait: true})
	if err != nil {
		return err
	}
	direct := strings.TrimSpace(resp.Out)
	ui.Println(ui.Dim("через VPN: ") + ui.Dash(viaVPN) + ui.Dim("   под группой "+GroupName+": ") + ui.Dash(direct))
	switch direct {
	case "":
		ui.Println(ui.Bad("под группой наружу не вышло: " + ui.Dash(resp.Err)))
		ui.Println(ui.Dim("снять: tim vpn app --off; ниже - что видит pf"))
		return AppStatus()
	case viaVPN:
		ui.Println(ui.Warn("страна та же: правило не сработало или обе дороги ведут в одну страну"))
	default:
		ui.Println(ui.OK("работает: процессы группы " + GroupName + " идут мимо VPN"))
	}
	return nil
}

func myGroups() []int {
	g, _ := os.Getgroups()
	return g
}

// AppBundle: путь к .app и его исполняемому файлу по имени, пути или части имени.
func AppBundle(name string) (bundle, exe string, err error) {
	if strings.Contains(name, "/") {
		bundle = name
		if i := strings.Index(name, ".app/"); i >= 0 {
			bundle = name[:i+4]
		}
	} else {
		want := strings.ToLower(strings.TrimSuffix(name, ".app"))
		var found []string
		for _, dir := range []string{"/Applications", filepath.Join(paths.Home(), "Applications"), "/System/Applications"} {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				base := strings.ToLower(strings.TrimSuffix(e.Name(), ".app"))
				if !strings.HasSuffix(e.Name(), ".app") || !strings.Contains(base, want) {
					continue
				}
				if base == want {
					found = append([]string{filepath.Join(dir, e.Name())}, found...)
				} else {
					found = append(found, filepath.Join(dir, e.Name()))
				}
			}
		}
		if len(found) == 0 {
			return "", "", ui.Errorf("приложение %s не нашёл в /Applications; укажи путь к .app", name)
		}
		bundle = found[0]
	}
	out, err := runner.Output("plutil", "-extract", "CFBundleExecutable", "raw", "-o", "-", filepath.Join(bundle, "Contents", "Info.plist"))
	if err != nil {
		return "", "", ui.Errorf("%s: не похоже на приложение (нет Info.plist)", bundle)
	}
	exe = filepath.Join(bundle, "Contents", "MacOS", strings.TrimSpace(out))
	if _, err := os.Stat(exe); err != nil {
		return "", "", ui.Errorf("исполняемый файл не найден: %s", exe)
	}
	return bundle, exe, nil
}

func AppLaunch(name string, closeRunning func(bundle, appName string) error) error {
	bundle, exe, err := AppBundle(name)
	if err != nil {
		return err
	}
	appName := strings.TrimSuffix(filepath.Base(bundle), ".app")
	gid, err := AppOn()
	if err != nil {
		return err
	}
	// Запущенный экземпляр новое окно откроет в старом процессе - под VPN. Сначала закрываем.
	if err := closeRunning(bundle, appName); err != nil {
		return err
	}
	resp, err := privileged.Launch(privileged.LaunchReq{Path: exe, Env: os.Environ(), Dir: paths.Home(),
		UID: os.Getuid(), GID: gid, Groups: myGroups()})
	if err != nil {
		return err
	}
	if resp.Err != "" {
		return ui.Errorf("%s не запустился: %s", appName, resp.Err)
	}
	ui.Println(ui.Label(appName+" запущен мимо VPN") + ui.Dim("  pid "+strconv.Itoa(resp.PID)+"; проверить: открой в нём ifconfig.co"))
	ui.Println(ui.Dim("вернуть под VPN: закрой приложение и открой из Dock; снять правило: tim vpn app --off"))
	return nil
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
