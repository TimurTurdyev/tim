package servers

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// expose: локальный порт наружу - через свой сервер (ssh -R), frps или cloudflared; Ctrl-C снимает.
func expose(env *app.Env, port string, o exposeOptions) error {
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return ui.Errorf("порт вне 1-65535: %s", port)
	}
	remote := o.remote
	if remote == 0 {
		remote, _ = strconv.Atoi(port)
	}
	switch {
	case o.ssh != "":
		return exposeSSH(env, port, o.ssh, remote)
	case o.frp != "":
		return exposeFRP(port, o.frp, remote)
	}
	if !runner.Has("cloudflared") && !runner.DryRun {
		return ui.Errorf("cloudflared не найден: %s (или --ssh сервер, --frp host:port)", runner.InstallHint("cloudflared", "https://pkg.cloudflare.com"))
	}
	ui.Println(ui.Dim("адрес вида https://*.trycloudflare.com cloudflared напечатает ниже"))
	return runner.Exec("cloudflared", "tunnel", "--no-autoupdate", "--url", "http://localhost:"+port)
}

func exposeSSH(env *app.Env, port, via string, remote int) error {
	server, err := sshTarget(env.Cfg, via)
	if err != nil {
		return err
	}
	ensureBypass(env.Cfg, server)
	ui.Println(ui.Dim(fmt.Sprintf("снаружи: %s:%d; на сервере нужен GatewayPorts yes в sshd_config", sshx.Resolve(server).Host, remote)))
	return runner.Exec("ssh", "-N", "-o", "ExitOnForwardFailure=yes", "-R", fmt.Sprintf("0.0.0.0:%d:localhost:%s", remote, port), server)
}

func exposeFRP(port, frps string, remote int) error {
	if !runner.Has("frpc") && !runner.DryRun {
		return ui.Errorf("frpc не найден: %s", runner.InstallHint("frpc", "https://github.com/fatedier/frp/releases"))
	}
	host, sport, ok := strings.Cut(frps, ":")
	if !ok {
		sport = "7000"
	}
	conf := fmt.Sprintf("serverAddr = %q\nserverPort = %s\n\n[[proxies]]\nname = \"tim-%s\"\ntype = \"tcp\"\nlocalIP = \"127.0.0.1\"\nlocalPort = %s\nremotePort = %d\n",
		host, sport, port, port, remote)
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
}
