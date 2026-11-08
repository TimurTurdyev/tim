package servers

import (
	"fmt"
	"os"
	"runtime"
	"strconv"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/report"
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

func (t *tunnels) socksPort() string {
	if t.port > 0 {
		return strconv.Itoa(t.port)
	}
	return strconv.Itoa(t.env.Cfg.Tunnel.Port)
}

// server: аргумент (алиас, user@host) или tunnel.host из конфига.
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

// tunnelRow: туннель для --json.
type tunnelRow struct {
	Name  string `json:"name"`
	Alive bool   `json:"alive"`
	Host  string `json:"host,omitempty"`
	Desc  string `json:"desc,omitempty"`
}

// tunnelsData: туннели tim и системный SOCKS - форма --json у tim tunnel.
type tunnelsData struct {
	Tunnels     []tunnelRow `json:"tunnels"`
	SystemSocks string      `json:"system_socks,omitempty"`
}

func tunnelsReport() report.Report {
	d := tunnelsData{Tunnels: []tunnelRow{}}
	t := report.Table{Columns: []report.Column{{Title: "ТУННЕЛЬ", Style: report.Key}, {Title: "СОСТОЯНИЕ"}, {Title: "КУДА"}, {Title: "ЧТО", Style: report.Muted}}}
	add := func(row tunnelRow) {
		state := report.Mark{Text: "работает", Level: report.OK}
		if !row.Alive {
			state = report.Mark{Text: "умер", Level: report.Bad}
		}
		d.Tunnels = append(d.Tunnels, row)
		t.Rows = append(t.Rows, []any{row.Name, state, row.Host, row.Desc})
	}
	for _, k := range sshx.Known() {
		add(tunnelRow{Name: k.Name, Alive: k.Alive(), Host: k.Host, Desc: k.Desc})
	}
	if pid, ok := sshx.ShuttlePID(); ok {
		add(tunnelRow{Name: "all", Alive: true, Desc: "sshuttle, pid " + pid + ", весь IPv4 через ssh"})
	}
	r := report.Report{Schema: "tim.tunnel/1", Data: &d, Blocks: []report.Block{t}}
	if len(d.Tunnels) == 0 {
		r.Blocks = []report.Block{report.Note("туннелей нет: tim tunnel up | port | all")}
	}
	if phys, _ := netinfo.Physical(); phys.Name != "" {
		if p := netinfo.SocksProxy(phys.Name); p.Enabled {
			d.SystemSocks = p.Server + ":" + p.Port
			r.Blocks = append(r.Blocks, report.Facts{{Key: "системный SOCKS", Value: "включён на " + phys.Name + ": " + d.SystemSocks, Level: report.Warn}})
		}
	}
	return r
}

// tunnelCheck: внешний адрес напрямую и через SOCKS - форма --json у tim tunnel check.
type tunnelCheck struct {
	Direct string `json:"direct,omitempty"`
	Socks  string `json:"socks,omitempty"`
	Same   bool   `json:"same"`
}

func (t *tunnels) checkReport() report.Report {
	var d tunnelCheck
	direct := report.Fact{Key: "напрямую"}
	if ip, err := netinfo.ExternalIP(""); err != nil {
		direct.Value, direct.Level = err.Error(), report.Bad
	} else {
		d.Direct, direct.Value = ip, ip
	}
	facts := report.Facts{direct}
	r := report.Report{Schema: "tim.tunnel.check/1", Data: &d}
	proxy := "socks5://127.0.0.1:" + t.socksPort()
	via, err := netinfo.ExternalIP(proxy)
	if err != nil {
		facts = append(facts, report.Fact{Key: "через SOCKS", Value: "туннеля нет или он не отвечает (" + proxy + ")", Level: report.Info})
		r.Blocks = []report.Block{facts}
		if _, ok := sshx.ShuttlePID(); ok {
			r.Blocks = append(r.Blocks, report.Note("tunnel all работает: адрес напрямую выше уже должен быть адресом сервера"))
		}
		return r
	}
	d.Socks, d.Same = via, via == d.Direct
	facts = append(facts, report.Fact{Key: "через SOCKS", Value: via})
	if d.Same {
		facts = append(facts, report.Fact{Key: "итог", Value: "адреса совпадают: трафик через SOCKS выходит тем же путём", Level: report.Warn})
	}
	r.Blocks = []report.Block{facts}
	return r
}
