package bypass

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/TimurTurdyev/tim/internal/machine"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/privileged"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Обход по приложению: программа запускается под группой novpn, и её трафик ядро уводит в
// физический интерфейс. Маршруты приложений не различают, а группу процесса различают и pf на
// macOS (через прозрачный прокси tim, app_pf.go), и netfilter на Linux (метка и своя таблица
// маршрутов, app_netfilter.go). Снаружи одно и то же: AppOn, AppOff, AppStatus, AppCheck, AppLaunch.

const GroupName = "novpn"

// GID: группа novpn, при первом разе заводится.
func GID() (int, error) {
	if runtime.GOOS == "linux" {
		return linuxGID()
	}
	return darwinGID()
}

// AppOn готовит группу и правила; возвращает gid, под которым запускать программы.
func AppOn() (int, error) {
	phys, gw := netinfo.Physical()
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
	if runtime.GOOS == "linux" {
		return gid, linuxOn(gid, phys, gw)
	}
	return gid, pfOn(phys, gw, tunnels)
}

// AppOff снимает правила; запущенные под группой программы снова пойдут через VPN.
func AppOff() error {
	if runtime.GOOS == "linux" {
		return linuxOff()
	}
	return pfOff()
}

// AppStatus: правила на месте ли, срабатывают ли, кто сейчас работает под группой.
func AppStatus(rules bool) error {
	kernel := "pf"
	if runtime.GOOS == "linux" {
		kernel = "netfilter"
	}
	switch {
	case !rules && !privileged.Ready():
		// Ядро без root правил не показывает: просмотр не должен спрашивать пароль.
		ui.Println(ui.Dim("правила " + kernel + ": tim direct --rules, спросит пароль администратора"))
	case runtime.GOOS == "linux":
		linuxStatus()
	default:
		pfStatus()
	}
	if runtime.GOOS != "linux" {
		if pid := ProxyPID(); pid > 0 {
			ui.Println(ui.Label("прокси ") + "работает, pid " + strconv.Itoa(pid) + ui.Dim("  журнал "+ProxyLogFile()))
		} else {
			ui.Println(ui.Dim("прокси не запущен"))
		}
	}
	gid, err := GID()
	if err != nil {
		ui.Println(ui.Dim("группы " + GroupName + " нет"))
		return nil
	}
	mine := runningUnder(gid)
	if len(mine) == 0 {
		ui.Println(ui.Dim("под группой " + GroupName + " сейчас никто не работает"))
		return nil
	}
	ui.Println(ui.Label("мимо VPN сейчас ") + strings.Join(mine, ", "))
	return nil
}

// runningUnder: имена программ, чьи процессы идут под группой.
func runningUnder(gid int) []string {
	ps, _ := runner.Output("ps", "-axo", "pid=,gid=,comm=")
	var mine []string
	for _, line := range strings.Split(ps, "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[1] == strconv.Itoa(gid) {
			mine = append(mine, procs.AppName(f[2]))
		}
	}
	return uniq(mine)
}

// AppCheck: правила на месте и действительно работают - curl под группой видит другую страну.
func AppCheck() error {
	gid, err := AppOn()
	if err != nil {
		return err
	}
	curl, err := runner.LookPath("curl")
	if err != nil {
		return ui.Errorf("для проверки нужен curl")
	}
	probe := []string{"-s", "--max-time", "8", "https://ifconfig.co/country"}
	viaVPN := strings.TrimSpace(func() string { o, _ := runner.Output(curl, probe...); return o }())
	stop := pfCaptureStart()
	resp, err := privileged.Launch(privileged.LaunchReq{Path: curl, Args: probe,
		Env: os.Environ(), Dir: paths.Home(), UID: os.Getuid(), GID: gid, Groups: myGroups(), Wait: true})
	if err != nil {
		return err
	}
	direct := strings.TrimSpace(resp.Out)
	ui.Println(ui.Dim("через VPN: ") + ui.Dash(viaVPN) + ui.Dim("   под группой "+GroupName+": ") + ui.Dash(direct))
	switch {
	case direct == "":
		ui.Println(ui.Bad("под группой наружу не вышло: " + ui.Dash(resp.Err)))
		ui.Println(ui.Dim("снять: tim direct off; ниже - что видит система"))
		err := AppStatus(true)
		stop(true)
		return err
	case viaVPN == "":
		stop(false)
		ui.Println(ui.Warn("через VPN замер не удался - сравнить не с чем, но под группой наружу выходит"))
	case direct == viaVPN:
		stop(false)
		ui.Println(ui.Warn("страна та же: правило не сработало или обе дороги ведут в одну страну"))
	default:
		stop(false)
		ui.Println(ui.OK("работает: процессы группы " + GroupName + " идут мимо VPN"))
	}
	return nil
}

func myGroups() []int {
	g, _ := os.Getgroups()
	return g
}

// AppBundle: что запускать. На macOS - .app по имени, части имени или пути; на Linux - команда
// из PATH или путь. Возвращает "дом" программы (для поиска запущенного экземпляра) и исполняемый файл.
func AppBundle(name string) (bundle, exe string, err error) {
	if runtime.GOOS != "darwin" {
		p, err := runner.LookPath(name)
		if err != nil {
			return "", "", ui.Errorf("программа %s не найдена в PATH; укажи путь", name)
		}
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		return p, p, nil
	}
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

// AppExists: по имени находится приложение - так tim direct отличает "Firefox" от сайта.
func AppExists(name string) bool {
	_, _, err := AppBundle(name)
	return err == nil
}

// AppLaunch запускает программу под группой novpn. Уже открытый экземпляр закрывается:
// иначе новое окно откроется в старом процессе, под VPN.
func AppLaunch(name string) error {
	bundle, exe, err := AppBundle(name)
	if err != nil {
		return err
	}
	appName := strings.TrimSuffix(filepath.Base(bundle), ".app")
	gid, err := AppOn()
	if err != nil {
		return err
	}
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
	ui.Println(ui.Dim("вернуть под VPN: закрой программу и открой как обычно; снять правила: tim direct off"))
	return nil
}

// closeRunning бережно закрывает открытый экземпляр приложения перед запуском мимо VPN.
func closeRunning(bundle, appName string) error {
	list, err := procs.All()
	if err != nil {
		return nil
	}
	for _, a := range procs.Find(list, appName) {
		if a.Main.Comm != bundle && !strings.HasPrefix(a.Main.Comm, bundle+"/") {
			continue
		}
		ui.Println(ui.Dim("закрываю " + a.Name + ": открытый экземпляр остался бы под VPN"))
		text, err := machine.Close(a, false)
		if err != nil {
			return ui.Errorf("%v; закрой %s сам и повтори", err, a.Name)
		}
		ui.Println(ui.Dim(text))
		time.Sleep(time.Second)
	}
	return nil
}

func alive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func firstLine(out string, err error) string {
	if out == "" && err != nil {
		return err.Error()
	}
	return strings.SplitN(out, "\n", 2)[0]
}

// prevFile: что было до нас (sysctl, маршрут) - direct off вернёт как было.
func prevFile(name string) string { return filepath.Join(GuardDir(), "novpn-"+name+".prev") }

func remember(name, value string) {
	if runner.DryRun {
		return
	}
	_ = runner.WriteAtomic(prevFile(name), []byte(value), 0o644)
}

func recall(name string) (string, bool) {
	data, err := os.ReadFile(prevFile(name))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(data)), true
}

func forget(name string) {
	if runner.DryRun {
		return
	}
	_ = os.Remove(prevFile(name))
}
