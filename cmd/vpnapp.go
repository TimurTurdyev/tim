package cmd

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// tim vpn app: приложение целиком мимо VPN. Таблица маршрутов приложений не различает, поэтому
// через pf: трафик процессов группы novpn уходит в физический интерфейс (route-to) с его
// адресом (nat). Приложение запускается под этой группой из tim; из Dock оно пойдёт как обычно.

const (
	novpnGroup = "novpn"
	pfAnchor   = "com.apple/tim" // внутри com.apple/*, который pf.conf macOS подключает сам
)

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
правило и прокси. Пароль sudo спрашивается один раз. Пока только macOS; на Linux - tim vpn unblock по сайтам.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS != "darwin" {
			return ui.Errorf("пока только macOS; на Linux - tim vpn unblock <сайт>")
		}
		switch {
		case appOff:
			return appRulesOff()
		case appStatus:
			return appStatusRun()
		case appCheck:
			return appCheckRun()
		case len(args) == 0:
			return ui.Errorf("какое приложение? tim vpn app Firefox, или --check, --status, --off")
		}
		return appLaunch(args[0])
	},
}

// novpnGID: группа novpn, при первом разе заводится.
func novpnGID() (int, error) {
	if out, err := runner.Output("dscl", ".", "-read", "/Groups/"+novpnGroup, "PrimaryGroupID"); err == nil {
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
	if _, err := rootRun("dscl", ".", "-create", "/Groups/"+novpnGroup, "PrimaryGroupID", strconv.Itoa(gid)); err != nil {
		return 0, ui.Errorf("группа %s не завелась: %v", novpnGroup, err)
	}
	_, _ = rootRun("dscl", ".", "-create", "/Groups/"+novpnGroup, "RealName", "tim: мимо VPN")
	ui.Println(ui.Label("группа "+novpnGroup) + ui.Dim("  gid "+strconv.Itoa(gid)))
	return gid, nil
}

// appRules: правила pf. Соединения процессов группы заворачиваются на lo0 и дальше (rdr) на прокси
// tim, который сам ходит к адресату через физический интерфейс. Локальные адреса не трогаем,
// иначе под раздачу попали бы dev-серверы на 127.0.0.1.
func appRules(tunnels []netinfo.Tunnel) string {
	var b strings.Builder
	b.WriteString("# tim: процессы группы " + novpnGroup + " идут мимо VPN через прокси tim\n")
	b.WriteString("table <tim_local> { 127.0.0.0/8, 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 169.254.0.0/16 }\n")
	fmt.Fprintf(&b, "rdr pass on lo0 inet proto tcp from any to ! <tim_local> -> 127.0.0.1 port %d\n", novpnPort)
	for _, t := range tunnels {
		fmt.Fprintf(&b, "pass out quick on %s route-to lo0 inet proto tcp from any to ! <tim_local> group %s keep state\n", t.Iface, novpnGroup)
	}
	return b.String()
}

// ifaceIPv4: адрес интерфейса - источник для исходящих соединений прокси.
func ifaceIPv4(dev string) string {
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

// novpnProxyUp: прокси под root живёт отдельно от tim; если уже работает - ничего не делаем.
func novpnProxyUp(phys netinfo.Service) error {
	if novpnProxyPID() > 0 {
		return nil
	}
	ip := ifaceIPv4(phys.Device)
	if ip == "" {
		return ui.Errorf("у %s нет IPv4-адреса", phys.Device)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	resp, err := rootCall(rootReq{Launch: &launchReq{Path: exe, Args: []string{"novpn-proxy", "--iface", phys.Device, "--ip", ip, "--port", strconv.Itoa(novpnPort)},
		Env: []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=/var/root"}, Dir: "/"}})
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
		if novpnProxyPID() > 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return ui.Errorf("прокси не поднялся, смотри %s", novpnLogFile())
}

// appRulesOn заводит группу, прокси и правила; возвращает gid.
func appRulesOn() (int, error) {
	phys, gw := physical()
	if phys.Device == "" || gw == "" {
		return 0, ui.Errorf("физический интерфейс или шлюз не найден: сети нет?")
	}
	tunnels := netinfo.Tunnels()
	if len(tunnels) == 0 {
		return 0, ui.Errorf("туннеля нет: VPN выключен, приложения и так идут напрямую")
	}
	gid, err := novpnGID()
	if err != nil {
		return 0, err
	}
	if err := novpnProxyUp(phys); err != nil {
		return 0, err
	}
	rules := appRules(tunnels)
	if runner.DryRun {
		ui.Println(ui.Dim(strings.TrimRight(rules, "\n")))
	}
	if out, err := rootRunStdin(rules, "pfctl", "-q", "-a", pfAnchor, "-f", "-"); err != nil {
		return 0, ui.Errorf("pf не принял правила: %s", firstLine(out, err))
	}
	if out, err := rootRun("pfctl", "-q", "-e"); err != nil && !strings.Contains(out+err.Error(), "already enabled") {
		return 0, ui.Errorf("pf не включился: %s", firstLine(out, err))
	}
	ui.Println(ui.Label("мимо VPN") + ui.Dim("  группа "+novpnGroup+", прокси 127.0.0.1:"+strconv.Itoa(novpnPort)+" через "+phys.Device+" "+ifaceIPv4(phys.Device)))
	return gid, nil
}

func firstLine(out string, err error) string {
	if out == "" && err != nil {
		return err.Error()
	}
	return strings.SplitN(out, "\n", 2)[0]
}

func appRulesOff() error {
	if _, err := rootRun("pfctl", "-q", "-a", pfAnchor, "-F", "all"); err != nil {
		return ui.Errorf("снять правила не вышло: %v", err)
	}
	if pid := novpnProxyPID(); pid > 0 {
		_, _ = rootRun("kill", strconv.Itoa(pid))
	}
	ui.Println(ui.Label("снято") + ui.Dim("  правила pf и прокси; запущенные под группой программы снова пойдут через VPN"))
	return nil
}

// appStatusRun: включён ли pf, какие правила стоят, кто сейчас работает под группой.
func appStatusRun() error {
	// Без -q: с ним pfctl молчит и про правила, и про состояния.
	if info, err := rootRun("pfctl", "-s", "info"); err == nil {
		ui.Println(ui.Label("pf ") + strings.TrimPrefix(firstLine(info, nil), "Status: "))
	}
	if pid := novpnProxyPID(); pid > 0 {
		ui.Println(ui.Label("прокси ") + "работает, pid " + strconv.Itoa(pid) + ui.Dim("  журнал "+novpnLogFile()))
	} else {
		ui.Println(ui.Dim("прокси не запущен"))
	}
	rules, err := rootRun("pfctl", "-a", pfAnchor, "-s", "all")
	switch {
	case err != nil:
		ui.Println(ui.Dim("правил pf нет: " + firstLine(rules, err)))
	case strings.TrimSpace(rules) == "":
		ui.Println(ui.Dim("правил pf нет: tim vpn app <приложение> или --check"))
	default:
		ui.Println(ui.Label("правила pf в " + pfAnchor))
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
	if states, err := rootRun("pfctl", "-s", "states"); err == nil {
		redir, tun := 0, 0
		for _, l := range strings.Split(states, "\n") {
			fromTunnel := false
			for _, ip := range tunIPs {
				fromTunnel = fromTunnel || strings.Contains(l, ip+":")
			}
			switch {
			case fromTunnel && strings.Contains(l, "127.0.0.1:"+strconv.Itoa(novpnPort)):
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
		o, _ := runner.Output("dscl", ".", "-read", "/Groups/"+novpnGroup, "PrimaryGroupID")
		_, v, _ := strings.Cut(strings.TrimSpace(o), ":")
		return v
	}()))
	if err != nil {
		ui.Println(ui.Dim("группы " + novpnGroup + " нет"))
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
		ui.Println(ui.Dim("под группой " + novpnGroup + " сейчас никто не работает"))
		return nil
	}
	ui.Println(ui.Label("мимо VPN сейчас ") + strings.Join(uniqStrings(mine), ", "))
	return nil
}

// appCheckRun: правило на месте и действительно работает - curl под группой видит другую страну.
func appCheckRun() error {
	gid, err := appRulesOn()
	if err != nil {
		return err
	}
	viaVPN := strings.TrimSpace(func() string {
		o, _ := runner.Output("curl", "-s", "--max-time", "8", "https://ifconfig.co/country")
		return o
	}())
	resp, err := rootCall(rootReq{Launch: &launchReq{Path: "/usr/bin/curl", Args: []string{"-s", "--max-time", "8", "https://ifconfig.co/country"},
		Env: os.Environ(), Dir: paths.Home(), UID: os.Getuid(), GID: gid, Groups: myGroups(), Wait: true}})
	if err != nil {
		return err
	}
	direct := strings.TrimSpace(resp.Out)
	ui.Println(ui.Dim("через VPN: ") + dashIfEmpty(viaVPN) + ui.Dim("   под группой "+novpnGroup+": ") + dashIfEmpty(direct))
	switch {
	case direct == "" && resp.Err != "":
		ui.Println(ui.Bad("под группой наружу не вышло: " + resp.Err))
		ui.Println(ui.Dim("так бывает, когда VPN-клиент сам ставит правила pf (kill switch); снять: tim vpn app --off"))
	case direct == "":
		ui.Println(ui.Bad("под группой ответа нет: пакеты ушли в " + "интерфейс, а обратно не вернулись; снять: tim vpn app --off"))
	case direct == viaVPN:
		ui.Println(ui.Warn("страна та же: правило не сработало или обе дороги ведут в одну страну"))
	default:
		ui.Println(ui.OK("работает: процессы группы " + novpnGroup + " идут мимо VPN"))
	}
	return nil
}

func myGroups() []int {
	g, _ := os.Getgroups()
	return g
}

// appBundle: путь к .app и его исполняемому файлу по имени, пути или части имени.
func appBundle(name string) (bundle, exe string, err error) {
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

func appLaunch(name string) error {
	bundle, exe, err := appBundle(name)
	if err != nil {
		return err
	}
	appName := strings.TrimSuffix(filepath.Base(bundle), ".app")
	gid, err := appRulesOn()
	if err != nil {
		return err
	}
	// Запущенный экземпляр новое окно откроет в старом процессе - под VPN. Сначала закрываем.
	if list, err := allProcs(); err == nil {
		for _, a := range procs.Find(list, appName) {
			if !a.IsApp || !strings.HasPrefix(a.Main.Comm, bundle+"/") {
				continue
			}
			ui.Println(ui.Dim("закрываю " + a.Name + ": открытый экземпляр остался бы под VPN"))
			if text, err := closeApp(a, false); err != nil {
				return ui.Errorf("%v; закрой %s сам и повтори", err, a.Name)
			} else {
				ui.Println(ui.Dim(text))
			}
			time.Sleep(time.Second)
		}
	}
	resp, err := rootCall(rootReq{Launch: &launchReq{Path: exe, Env: os.Environ(), Dir: paths.Home(),
		UID: os.Getuid(), GID: gid, Groups: myGroups()}})
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

func init() {
	vpnAppCmd.Flags().BoolVar(&appOff, "off", false, "снять правило pf")
	vpnAppCmd.Flags().BoolVar(&appStatus, "status", false, "правила и кто сейчас работает мимо VPN")
	vpnAppCmd.Flags().BoolVar(&appCheck, "check", false, "завести правило и проверить его на curl")
	vpnCmd.AddCommand(vpnAppCmd)
}
