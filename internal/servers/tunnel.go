package servers

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/dbx"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// tunnels: команды tim tunnel и общий для них флаг --port.
type tunnels struct {
	env  *app.Env
	port int
}

// allOptions: что оставить мимо sshuttle.
type allOptions struct {
	noDNS   bool
	exclude []string
}

func printTunnels() {
	list := sshx.Known()
	if len(list) == 0 {
		ui.Println(ui.Dim("туннелей нет: tim tunnel up | port | db"))
	}
	for _, t := range list {
		state := ui.Label("работает")
		if !t.Alive() {
			state = ui.Bad("умер")
		}
		ui.Println(ui.Pad(t.Name, 14) + " " + ui.Pad(state, 10) + " " + t.Host + "  " + ui.Dim(t.Desc))
	}
	if pid, ok := sshx.ShuttlePID(); ok {
		ui.Println(ui.Pad("all", 14) + " " + ui.Pad(ui.Label("работает"), 10) + " " + ui.Dim("sshuttle, pid "+pid+", весь IPv4 через ssh"))
	}
	if phys, _ := netinfo.Physical(); phys.Name != "" {
		if p := netinfo.SocksProxy(phys.Name); p.Enabled {
			ui.Println(ui.Warn("системный SOCKS включён на " + phys.Name + ": " + p.Server + ":" + p.Port))
		}
	}
}

func (t *tunnels) socksPort() string {
	if t.port > 0 {
		return strconv.Itoa(t.port)
	}
	return strconv.Itoa(t.env.Cfg.Tunnel.Port)
}

// server: аргумент (алиас, проект, user@host) или tunnel.host из конфига.
func (t *tunnels) server(arg string) (string, error) {
	if arg == "" {
		arg = t.env.Cfg.Tunnel.Host
	}
	if arg == "" {
		return "", ui.Errorf("укажи сервер или задай tunnel.host в конфиге")
	}
	return sshTarget(t.env.Cfg, arg)
}

func (t *tunnels) socks(server string) sshx.Tunnel {
	return sshx.Tunnel{Name: "socks", Host: server, Desc: "SOCKS5 127.0.0.1:" + t.socksPort() + " через " + server}
}

func (t *tunnels) up(arg string) error {
	server, err := t.server(arg)
	if err != nil {
		return err
	}
	s := t.socks(server)
	if !s.Alive() {
		if err := localFree(t.socksPort()); err != nil {
			return err
		}
	}
	return t.start(s, "-D", "127.0.0.1:"+t.socksPort())
}

func (t *tunnels) down(want string) error {
	if want == "" {
		want = "socks"
	}
	found := false
	for _, k := range sshx.Known() {
		if want != "all" && k.Name != want {
			continue
		}
		found = true
		k.Stop()
		if runner.DryRun {
			continue
		}
		ui.Println(ui.Label(k.Name + " снят"))
		if k.Name == "socks" {
			systemProxy(false, "")
		}
	}
	if !found {
		return ui.Errorf("туннеля %s нет: tim tunnel", want)
	}
	return nil
}

func (t *tunnels) check() {
	direct, err := netinfo.ExternalIP("")
	if err != nil {
		direct = ui.Bad(err.Error())
	}
	ui.Println(ui.Label("напрямую  ") + direct)
	proxy := "socks5://127.0.0.1:" + t.socksPort()
	via, err := netinfo.ExternalIP(proxy)
	if err != nil {
		ui.Println(ui.Dim("через SOCKS: туннеля нет или он не отвечает (" + proxy + ")"))
		if _, ok := sshx.ShuttlePID(); ok {
			ui.Println(ui.Dim("tunnel all работает: адрес напрямую выше уже должен быть адресом сервера"))
		}
		return
	}
	ui.Println(ui.Label("через SOCKS ") + via)
	if via == direct {
		ui.Println(ui.Warn("адреса совпадают: трафик через SOCKS выходит тем же путём"))
	}
}

func (t *tunnels) system(args []string) error {
	switch args[0] {
	case "off":
		systemProxy(false, "")
		return nil
	case "on":
		if runtime.GOOS != "darwin" {
			return ui.Errorf("общего системного прокси на Linux нет; подними tim tunnel up и задай программам ALL_PROXY=socks5h://127.0.0.1:%s", t.socksPort())
		}
		arg := ""
		if len(args) > 1 {
			arg = args[1]
		}
		server, err := t.server(arg)
		if err != nil {
			return err
		}
		if err := t.start(t.socks(server), "-D", "127.0.0.1:"+t.socksPort()); err != nil {
			return err
		}
		systemProxy(true, t.socksPort())
		return nil
	}
	return ui.Errorf("on или off")
}

func (t *tunnels) forward(args []string) error {
	f, err := sshx.ParseForward(args[0])
	if err != nil {
		return err
	}
	arg := ""
	if len(args) > 1 {
		arg = args[1]
	}
	server, err := t.server(arg)
	if err != nil {
		return err
	}
	if err := localFree(f.Local); err != nil {
		return err
	}
	k := sshx.Tunnel{Name: "port-" + f.Local, Host: server, Desc: fmt.Sprintf("127.0.0.1:%s -> %s:%s на стороне %s", f.Local, f.Host, f.Remote, server)}
	return t.start(k, "-L", f.Arg())
}

// database: туннель к базе проекта для GUI-клиента; живёт до tunnel down.
func (t *tunnels) database(name, via string) error {
	p, err := t.env.Project(name)
	if err != nil {
		return err
	}
	c, err := dbx.Load(t.env.Cfg.ProjectDir(p))
	if err != nil {
		return ui.Errorf("%s: %v", p.Name, err)
	}
	if c.Driver == "sqlite" {
		return ui.Errorf("у %s sqlite, туннель не нужен", p.Name)
	}
	if via == "" {
		via = p.SSH
	}
	if via == "" {
		return ui.Errorf("у %s нет ssh в конфиге: --via <сервер>", p.Name)
	}
	server, err := sshTarget(t.env.Cfg, via)
	if err != nil {
		return err
	}
	local := strconv.Itoa(t.port)
	if t.port == 0 {
		if local, err = netinfo.FreePort(); err != nil {
			return err
		}
	}
	if err := localFree(local); err != nil {
		return err
	}
	k := sshx.Tunnel{Name: "db-" + p.Name, Host: server, Desc: fmt.Sprintf("127.0.0.1:%s -> %s:%s на стороне %s", local, c.Host, c.Port, server)}
	if err := t.start(k, "-L", "127.0.0.1:"+local+":"+c.Host+":"+c.Port); err != nil {
		return err
	}
	ui.Println(ui.Label("для клиента ") + fmt.Sprintf("%s  host 127.0.0.1  port %s  база %s  пользователь %s", c.Driver, local, c.Name, c.User))
	ui.Println(ui.Dim("пароль - в .env проекта; снять: tim tunnel down db-" + p.Name))
	return nil
}

// all: весь трафик через sshuttle; off - выключить.
func (t *tunnels) all(arg string, o allOptions) error {
	if arg == "off" {
		return shuttleOff()
	}
	for _, x := range o.exclude {
		if _, err := netinfo.ParseTarget(x); err != nil {
			return err
		}
	}
	if pid, ok := sshx.ShuttlePID(); ok {
		ui.Println(ui.Dim("уже работает, pid " + pid + ": tim tunnel all off"))
		return nil
	}
	if !runner.Has("sshuttle") && !runner.DryRun {
		return ui.Errorf("sshuttle не найден: %s", runner.InstallHint("sshuttle", "sshuttle"))
	}
	server, err := t.server(arg)
	if err != nil {
		return err
	}
	ensureBypass(t.env.Cfg, server)
	if err := runner.Run("sshuttle", sshx.ShuttleArgs(server, sshx.IP(server), !o.noDNS, o.exclude, sshx.ShuttlePidfile())...); err != nil {
		return ui.Errorf("sshuttle не поднялся: %v", err)
	}
	ui.Println(ui.Label("весь трафик идёт через "+server) + ui.Dim("  проверить: tim tunnel check, выключить: tim tunnel all off"))
	return nil
}

func shuttleOff() error {
	pid, ok := sshx.ShuttlePID()
	if !ok {
		ui.Println(ui.Dim("tunnel all не запущен"))
		return nil
	}
	if err := runner.Sudo("kill", pid); err != nil {
		return err
	}
	if !runner.DryRun {
		_ = os.Remove(sshx.ShuttlePidfile())
	}
	ui.Println(ui.Label("tunnel all выключен"))
	return nil
}

func (t *tunnels) start(k sshx.Tunnel, forward ...string) error {
	ensureBypass(t.env.Cfg, k.Host)
	if k.Alive() {
		ui.Println(ui.Dim(k.Name + " уже работает"))
		return nil
	}
	if err := k.Start(forward...); err != nil {
		return ui.Errorf("туннель до %s не поднялся: %v", k.Host, err)
	}
	ui.Println(ui.Label(k.Name+" поднят ") + ui.Dim(k.Desc))
	return nil
}

// localFree: локальный порт свободен, иначе ошибка с тем, кто его держит.
func localFree(port string) error {
	if l, busy := netinfo.PortBusy(port); busy {
		return ui.Errorf("локальный порт %s занят: %s (pid %s); возьми другой, например %s1:...", port, l.Command, l.PID, port)
	}
	return nil
}

// systemProxy включает или выключает системный SOCKS у активной службы macOS.
func systemProxy(on bool, port string) {
	if runtime.GOOS != "darwin" {
		return
	}
	phys, _ := netinfo.Physical()
	if phys.Name == "" {
		ui.Println(ui.Warn("активная сетевая служба не найдена"))
		return
	}
	if !on {
		if !netinfo.SocksProxy(phys.Name).Enabled && !runner.DryRun {
			return
		}
		if err := netinfo.DisableSocksProxy(phys.Name); err == nil {
			ui.Println(ui.Label("системный SOCKS выключен на " + phys.Name))
		}
		return
	}
	if err := netinfo.SetSocksProxy(phys.Name, port); err != nil {
		ui.Println(ui.Bad(err.Error()))
		return
	}
	ui.Println(ui.Label("системный SOCKS включён на " + phys.Name + " -> 127.0.0.1:" + port))
}
