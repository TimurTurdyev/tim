package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var tunnelPort int

var tunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Туннели через ssh: SOCKS, системный прокси, порты, база, expose",
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if pid, ok := shuttlePID(); ok {
			ui.Println(ui.Pad("all", 14) + " " + ui.Pad(ui.Label("работает"), 10) + " " + ui.Dim("sshuttle, pid "+pid+", весь IPv4 через ssh"))
		}
		if phys, _ := bypass.Physical(); phys.Name != "" {
			p := netinfo.SocksProxy(phys.Name)
			if p.Enabled {
				ui.Println(ui.Warn("системный SOCKS включён на " + phys.Name + ": " + p.Server + ":" + p.Port))
			}
		}
		return nil
	},
}

// tunnelServer: аргумент (алиас, проект, user@host) или tunnel.host из конфига.
func tunnelServer(arg string) (string, error) {
	if arg == "" {
		arg = cfg.Tunnel.Host
	}
	if arg == "" {
		return "", ui.Errorf("укажи сервер или задай tunnel.host в конфиге")
	}
	return sshTarget(arg)
}

func second(args []string) string {
	if len(args) > 1 {
		return args[1]
	}
	return ""
}

func socksPort() int {
	if tunnelPort > 0 {
		return tunnelPort
	}
	return cfg.Tunnel.Port
}

// localFree: локальный порт свободен, иначе ошибка с тем, кто его держит.
func localFree(port string) error {
	if l, busy := netinfo.PortBusy(port); busy {
		return ui.Errorf("локальный порт %s занят: %s (pid %s); возьми другой, например %s1:...", port, l.Command, l.PID, port)
	}
	return nil
}

func startTunnel(t sshx.Tunnel, forward ...string) error {
	if ip := sshx.IP(t.Host); ip != "" {
		ensureBypass(ip)
	}
	if t.Alive() {
		ui.Println(ui.Dim(t.Name + " уже работает"))
		return nil
	}
	if err := t.Start(forward...); err != nil {
		return ui.Errorf("туннель до %s не поднялся: %v", t.Host, err)
	}
	ui.Println(ui.Label(t.Name+" поднят ") + ui.Dim(t.Desc))
	return nil
}

func socksTunnel(server string) sshx.Tunnel {
	port := strconv.Itoa(socksPort())
	return sshx.Tunnel{Name: "socks", Host: server, Desc: "SOCKS5 127.0.0.1:" + port + " через " + server}
}

var tunnelUpCmd = &cobra.Command{
	Use:   "up [сервер] [--port 1080]",
	Short: "SOCKS5 через сервер (ssh -D): слушает только 127.0.0.1, наружу идёт обычный ssh",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		server, err := tunnelServer(first(args))
		if err != nil {
			return err
		}
		t := socksTunnel(server)
		if !t.Alive() {
			if err := localFree(strconv.Itoa(socksPort())); err != nil {
				return err
			}
		}
		return startTunnel(t, "-D", "127.0.0.1:"+strconv.Itoa(socksPort()))
	},
}

var tunnelDownCmd = &cobra.Command{
	Use:   "down [имя|all]",
	Short: "Снять туннель; без имени - SOCKS",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		want := first(args)
		if want == "" {
			want = "socks"
		}
		found := false
		for _, t := range sshx.Known() {
			if want != "all" && t.Name != want {
				continue
			}
			found = true
			if runner.DryRun {
				ui.Println(ui.Dim("$ ssh -S <сокет " + t.Name + "> -O exit " + t.Host))
				continue
			}
			t.Stop()
			ui.Println(ui.Label(t.Name + " снят"))
			if t.Name == "socks" {
				systemProxy(false)
			}
		}
		if !found {
			return ui.Errorf("туннеля %s нет: tim tunnel", want)
		}
		return nil
	},
}

var tunnelCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Внешний IP напрямую и через SOCKS",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		direct, err := externalIP()
		if err != nil {
			direct = ui.Bad(err.Error())
		}
		ui.Println(ui.Label("напрямую  ") + direct)
		proxy := "socks5://127.0.0.1:" + strconv.Itoa(socksPort())
		via, err := ipVia(proxy)
		if err != nil {
			ui.Println(ui.Dim("через SOCKS: туннеля нет или он не отвечает (" + proxy + ")"))
			if _, ok := shuttlePID(); ok {
				ui.Println(ui.Dim("tunnel all работает: адрес напрямую выше уже должен быть адресом сервера"))
			}
			return nil
		}
		ui.Println(ui.Label("через SOCKS ") + via)
		if via == direct {
			ui.Println(ui.Warn("адреса совпадают: трафик через SOCKS выходит тем же путём"))
		}
		return nil
	},
}

func ipVia(proxy string) (string, error) {
	u, err := url.Parse(proxy)
	if err != nil {
		return "", err
	}
	client := &http.Client{Timeout: 6 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(u)}}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	return strings.TrimSpace(string(b)), err
}

// systemProxy включает или выключает системный SOCKS у активной службы.
func systemProxy(on bool) {
	if runtime.GOOS != "darwin" {
		return
	}
	phys, _ := bypass.Physical()
	if phys.Name == "" {
		ui.Println(ui.Warn("активная сетевая служба не найдена"))
		return
	}
	if !on {
		if !netinfo.SocksProxy(phys.Name).Enabled && !runner.DryRun {
			return
		}
		if err := runner.Run("networksetup", "-setsocksfirewallproxystate", phys.Name, "off"); err == nil {
			ui.Println(ui.Label("системный SOCKS выключен на " + phys.Name))
		}
		return
	}
	port := strconv.Itoa(socksPort())
	if err := runner.Run("networksetup", "-setsocksfirewallproxy", phys.Name, "127.0.0.1", port); err != nil {
		ui.Println(ui.Bad("не вышло: " + err.Error()))
		return
	}
	if err := runner.Run("networksetup", "-setsocksfirewallproxystate", phys.Name, "on"); err != nil {
		ui.Println(ui.Bad("прокси прописан, но не включился: " + err.Error()))
		return
	}
	ui.Println(ui.Label("системный SOCKS включён на " + phys.Name + " -> 127.0.0.1:" + port))
}

var tunnelSystemCmd = &cobra.Command{
	Use:       "system on|off [сервер]",
	Short:     "Пустить весь системный трафик через SOCKS (настройки прокси macOS)",
	Args:      cobra.RangeArgs(1, 2),
	ValidArgs: []string{"on", "off"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "off":
			systemProxy(false)
			return nil
		case "on":
			if runtime.GOOS != "darwin" {
				return ui.Errorf("общего системного прокси на Linux нет; подними tim tunnel up и задай программам ALL_PROXY=socks5h://127.0.0.1:%d", socksPort())
			}
			server, err := tunnelServer(second(args))
			if err != nil {
				return err
			}
			if err := startTunnel(socksTunnel(server), "-D", "127.0.0.1:"+strconv.Itoa(socksPort())); err != nil {
				return err
			}
			systemProxy(true)
			return nil
		}
		return ui.Errorf("on или off")
	},
}

var tunnelPortCmd = &cobra.Command{
	Use:   "port <L:host:R | L:R | R> [сервер]",
	Short: "Пробросить порт с сервера на 127.0.0.1 (ssh -L)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		f, err := sshx.ParseForward(args[0])
		if err != nil {
			return err
		}
		server, err := tunnelServer(second(args))
		if err != nil {
			return err
		}
		if err := localFree(f.Local); err != nil {
			return err
		}
		t := sshx.Tunnel{Name: "port-" + f.Local, Host: server, Desc: fmt.Sprintf("127.0.0.1:%s -> %s:%s на стороне %s", f.Local, f.Host, f.Remote, server)}
		return startTunnel(t, "-L", f.Arg())
	},
}

var tunnelVia string

var tunnelDBCmd = &cobra.Command{
	Use:               "db [проект] [--via сервер] [--port L]",
	Short:             "Туннель к базе проекта для GUI-клиента; живёт до tunnel down",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := projectFor(first(args))
		if err != nil {
			return err
		}
		c, err := loadDB(p, cfg.ProjectDir(p))
		if err != nil {
			return err
		}
		if c.Driver == "sqlite" {
			return ui.Errorf("у %s sqlite, туннель не нужен", p.Name)
		}
		via := tunnelVia
		if via == "" {
			via = p.SSH
		}
		if via == "" {
			return ui.Errorf("у %s нет ssh в конфиге: --via <сервер>", p.Name)
		}
		server, err := sshTarget(via)
		if err != nil {
			return err
		}
		local := strconv.Itoa(tunnelPort)
		if tunnelPort == 0 {
			if local, err = freePort(); err != nil {
				return err
			}
		}
		if err := localFree(local); err != nil {
			return err
		}
		t := sshx.Tunnel{Name: "db-" + p.Name, Host: server, Desc: fmt.Sprintf("127.0.0.1:%s -> %s:%s на стороне %s", local, c.Host, c.Port, server)}
		if err := startTunnel(t, "-L", "127.0.0.1:"+local+":"+c.Host+":"+c.Port); err != nil {
			return err
		}
		ui.Println(ui.Label("для клиента ") + fmt.Sprintf("%s  host 127.0.0.1  port %s  база %s  пользователь %s", c.Driver, local, c.Name, c.User))
		ui.Println(ui.Dim("пароль - в .env проекта; снять: tim tunnel down db-" + p.Name))
		return nil
	},
}

var (
	exposeFRP    string
	exposeSSH    string
	exposeRemote int
)

var tunnelExposeCmd = &cobra.Command{
	Use:   "expose <порт> [--frp host:port | --ssh сервер] [--remote N]",
	Short: "Показать локальный порт наружу: cloudflared, frpc или ssh -R; Ctrl-C снимает",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		port := args[0]
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return ui.Errorf("порт вне 1-65535: %s", port)
		}
		remote := exposeRemote
		if remote == 0 {
			remote, _ = strconv.Atoi(port)
		}
		switch {
		case exposeSSH != "":
			server, err := sshTarget(exposeSSH)
			if err != nil {
				return err
			}
			if ip := sshx.IP(server); ip != "" {
				ensureBypass(ip)
			}
			ui.Println(ui.Dim(fmt.Sprintf("снаружи: %s:%d; на сервере нужен GatewayPorts yes в sshd_config", sshx.Resolve(server).Host, remote)))
			return runner.Exec("ssh", "-N", "-o", "ExitOnForwardFailure=yes", "-R", fmt.Sprintf("0.0.0.0:%d:localhost:%s", remote, port), server)
		case exposeFRP != "":
			if !runner.Has("frpc") && !runner.DryRun {
				return ui.Errorf("frpc не найден: %s", runner.InstallHint("frpc", "https://github.com/fatedier/frp/releases"))
			}
			host, sport, ok := strings.Cut(exposeFRP, ":")
			if !ok {
				sport = "7000"
			}
			conf := fmt.Sprintf("serverAddr = %q\nserverPort = %s\n\n[[proxies]]\nname = \"tim-%s\"\ntype = \"tcp\"\nlocalIP = \"127.0.0.1\"\nlocalPort = %s\nremotePort = %d\n", host, sport, port, port, remote)
			f, err := os.CreateTemp("", "tim-frpc-*.toml")
			if err != nil {
				return err
			}
			defer func() { _ = os.Remove(f.Name()) }()
			if _, err := f.WriteString(conf); err != nil {
				return err
			}
			_ = f.Close()
			ui.Println(ui.Dim(fmt.Sprintf("снаружи: %s:%d", host, remote)))
			return runner.Exec("frpc", "-c", f.Name())
		default:
			if !runner.Has("cloudflared") && !runner.DryRun {
				return ui.Errorf("cloudflared не найден: %s (или --ssh сервер, --frp host:port)", runner.InstallHint("cloudflared", "https://pkg.cloudflare.com"))
			}
			ui.Println(ui.Dim("адрес вида https://*.trycloudflare.com cloudflared напечатает ниже"))
			return runner.Exec("cloudflared", "tunnel", "--no-autoupdate", "--url", "http://localhost:"+port)
		}
	},
}

// shuttlePID: pid работающего sshuttle из pidfile.
func shuttlePID() (string, bool) {
	data, err := os.ReadFile(sshx.ShuttlePidfile())
	if err != nil {
		return "", false
	}
	pid := strings.TrimSpace(string(data))
	if _, err := runner.Output("ps", "-p", pid); err != nil {
		return pid, false
	}
	return pid, true
}

var (
	allNoDNS   bool
	allExclude []string
)

var tunnelAllCmd = &cobra.Command{
	Use:   "all [сервер|off] [--no-dns] [-x подсеть...]",
	Short: "Весь трафик через ssh без настройки прокси (sshuttle); off - выключить",
	Long: `Прозрачный туннель поверх ssh: sshuttle перехватывает весь IPv4 и DNS и отправляет через сервер.
Приложениям ничего настраивать не надо, на сервере нужен только python3.
Мимо туннеля остаются сам сервер и частные сети (локальная сеть и ресурсы корпоративного VPN).
IPv6 sshuttle не перехватывает. sudo спросит пароль при старте.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if first(args) == "off" {
			pid, ok := shuttlePID()
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
		for _, x := range allExclude {
			if _, err := netinfo.ParseTarget(x); err != nil {
				return err
			}
		}
		if pid, ok := shuttlePID(); ok {
			ui.Println(ui.Dim("уже работает, pid " + pid + ": tim tunnel all off"))
			return nil
		}
		if !runner.Has("sshuttle") && !runner.DryRun {
			return ui.Errorf("sshuttle не найден: %s", runner.InstallHint("sshuttle", "sshuttle"))
		}
		server, err := tunnelServer(first(args))
		if err != nil {
			return err
		}
		ip := sshx.IP(server)
		if ip != "" {
			ensureBypass(ip)
		}
		if err := runner.Run("sshuttle", sshx.ShuttleArgs(server, ip, !allNoDNS, allExclude, sshx.ShuttlePidfile())...); err != nil {
			return ui.Errorf("sshuttle не поднялся: %v", err)
		}
		ui.Println(ui.Label("весь трафик идёт через "+server) + ui.Dim("  проверить: tim tunnel check, выключить: tim tunnel all off"))
		return nil
	},
}

func init() {
	tunnelAllCmd.Flags().BoolVar(&allNoDNS, "no-dns", false, "DNS не заворачивать")
	tunnelAllCmd.Flags().StringSliceVarP(&allExclude, "exclude", "x", nil, "ещё подсети или адреса мимо туннеля")
	tunnelUpCmd.Flags().IntVar(&tunnelPort, "port", 0, "локальный порт SOCKS (по умолчанию tunnel.port)")
	tunnelSystemCmd.Flags().IntVar(&tunnelPort, "port", 0, "локальный порт SOCKS")
	tunnelCheckCmd.Flags().IntVar(&tunnelPort, "port", 0, "локальный порт SOCKS")
	tunnelDBCmd.Flags().StringVar(&tunnelVia, "via", "", "сервер вместо ssh проекта")
	tunnelDBCmd.Flags().IntVar(&tunnelPort, "port", 0, "локальный порт (по умолчанию свободный)")
	tunnelExposeCmd.Flags().StringVar(&exposeFRP, "frp", "", "сервер frps host[:port]")
	tunnelExposeCmd.Flags().StringVar(&exposeSSH, "ssh", "", "сервер для ssh -R")
	tunnelExposeCmd.Flags().IntVar(&exposeRemote, "remote", 0, "порт снаружи (по умолчанию тот же)")
	tunnelCmd.AddCommand(tunnelAllCmd, tunnelUpCmd, tunnelDownCmd, tunnelCheckCmd, tunnelSystemCmd, tunnelPortCmd, tunnelDBCmd, tunnelExposeCmd)
	root.AddCommand(tunnelCmd)
}
