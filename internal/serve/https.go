package serve

import (
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/hostsfile"
	"github.com/TimurTurdyev/tim/internal/localca"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/run"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tlsproxy"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// httpsPortBase: 443 на loopback без root не занять, а на всех адресах его часто держит OrbStack
// под свои *.orb.local. Поэтому прокси слушает 127.0.0.1 с этого порта и выше.
const httpsPortBase = "8443"

// httpsFront: https-прокси перед dev-сервером - имя, сертификат из tim link и порт.
type httpsFront struct {
	host, cert, key, port string
}

func (f httpsFront) url() string { return "https://" + f.host + ":" + f.port }

// planHTTPS: имя и сертификат из tim link, свободный порт для прокси. taken - порты, уже выданные
// в этом запуске: запущенные рядом процессы могли ещё не начать слушать.
func planHTTPS(p *config.Project, taken map[string]bool) (httpsFront, error) {
	host := hostsfile.LocalName(p.Name)
	if u, err := url.Parse(p.URL); err == nil && u.Scheme == "https" && u.Hostname() != "" {
		host = u.Hostname()
	}
	cert, key := localca.LeafPaths(host)
	if _, err := os.Stat(cert); err != nil {
		return httpsFront{}, ui.Errorf("для %s нет сертификата %s: сначала tim link %s", p.Name, host, p.Name)
	}
	busy := netinfo.BusyPorts()
	port := run.AllocPorts([]string{httpsPortBase}, func(s string) bool { return busy.Busy(s) || taken[s] })[0]
	taken[port] = true
	return httpsFront{host: host, cert: cert, key: key, port: port}, nil
}

// startHTTPSInline поднимает прокси в этом же процессе: для сервера на переднем плане.
func startHTTPSInline(f httpsFront, devPort string) error {
	if runner.DryRun {
		runner.Note("прокси " + f.url() + " -> http://127.0.0.1:" + devPort)
		return nil
	}
	target := &url.URL{Scheme: "http", Host: "127.0.0.1:" + devPort}
	serve, err := tlsproxy.Listen("127.0.0.1:"+f.port, target, f.cert, f.key)
	if err != nil {
		return ui.Errorf("https-прокси: %v", err)
	}
	go func() {
		if err := serve(); err != nil {
			ui.Println(ui.Bad("https-прокси упал: " + err.Error()))
		}
	}()
	return nil
}

// startHTTPSBackground поднимает прокси отдельным процессом рядом с фоновым сервером;
// tim serve stop <проект> гасит обоих.
func startHTTPSBackground(dir, project string, f httpsFront, devPort string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	name := project + ".https"
	logPath := run.LogPath(name)
	args := []string{"tls-proxy", "--listen", "127.0.0.1:" + f.port, "--to", "http://127.0.0.1:" + devPort, "--cert", f.cert, "--key", f.key}
	pid, err := runner.Start(dir, nil, logPath, self, args...)
	if err != nil {
		return err
	}
	if runner.DryRun {
		return nil
	}
	st := run.State{Name: name, PID: pid, Port: f.port, Dir: dir, Command: "tim " + strings.Join(args, " "), Log: logPath, Started: time.Now()}
	return st.Save()
}

func httpsHint(stack string) string {
	if stack == "laravel" {
		return "Laravel должен доверять прокси, иначе ссылки останутся http: TrustProxies с at = '*' или APP_URL=https://..."
	}
	return ""
}

// serveTLS: сам прокси для tim serve --https -d - отдельный процесс рядом с фоновым сервером.
func serveTLS(listen, to, cert, key string) error {
	target, err := url.Parse(to)
	if err != nil {
		return err
	}
	serve, err := tlsproxy.Listen(listen, target, cert, key)
	if err != nil {
		return err
	}
	return serve()
}
