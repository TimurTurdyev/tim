package cmd

import (
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
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

var serveHTTPS bool

type httpsFront struct {
	host, cert, key, port string
}

// planHTTPS: имя и сертификат из tim link, свободный порт для прокси. taken - порты, уже выданные
// в этом запуске: запущенные рядом процессы могли ещё не начать слушать.
func planHTTPS(p *config.Project, taken map[string]bool) (httpsFront, error) {
	host := strings.ToLower(p.Name) + ".test"
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

func (f httpsFront) url() string { return "https://" + f.host + ":" + f.port }

// startHTTPSInline поднимает прокси в этом же процессе: для сервера на переднем плане.
func startHTTPSInline(f httpsFront, devPort string) error {
	if runner.DryRun {
		ui.Println(ui.Dim("$ # прокси " + f.url() + " -> http://127.0.0.1:" + devPort))
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
func startHTTPSBackground(p *config.Project, f httpsFront, devPort string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	name := p.Name + ".https"
	logPath := run.LogPath(name)
	args := []string{"tls-proxy", "--listen", "127.0.0.1:" + f.port, "--to", "http://127.0.0.1:" + devPort, "--cert", f.cert, "--key", f.key}
	pid, err := runner.Start(cfg.ProjectDir(p), nil, logPath, self, args...)
	if err != nil {
		return err
	}
	if runner.DryRun {
		return nil
	}
	st := run.State{Name: name, PID: pid, Port: f.port, Dir: cfg.ProjectDir(p), Command: "tim " + strings.Join(args, " "), Log: logPath, Started: time.Now()}
	return st.Save()
}

func httpsHint(stack string) string {
	if stack == "laravel" {
		return "Laravel должен доверять прокси, иначе ссылки останутся http: TrustProxies с at = '*' или APP_URL=https://..."
	}
	return ""
}

var tlsProxyListen, tlsProxyTo, tlsProxyCert, tlsProxyKey string

var tlsProxyCmd = &cobra.Command{
	Use:    "tls-proxy --listen 127.0.0.1:8443 --to http://127.0.0.1:8000 --cert f --key f",
	Short:  "HTTPS-прокси перед dev-сервером (для tim serve --https -d)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		target, err := url.Parse(tlsProxyTo)
		if err != nil {
			return err
		}
		serve, err := tlsproxy.Listen(tlsProxyListen, target, tlsProxyCert, tlsProxyKey)
		if err != nil {
			return err
		}
		return serve()
	},
}

func init() {
	tlsProxyCmd.Flags().StringVar(&tlsProxyListen, "listen", "127.0.0.1:"+httpsPortBase, "где слушать")
	tlsProxyCmd.Flags().StringVar(&tlsProxyTo, "to", "", "куда проксировать")
	tlsProxyCmd.Flags().StringVar(&tlsProxyCert, "cert", "", "сертификат")
	tlsProxyCmd.Flags().StringVar(&tlsProxyKey, "key", "", "ключ")
	serveCmd.Flags().BoolVar(&serveHTTPS, "https", false, "отдать по https://имя.test через свой сертификат (после tim link)")
	root.AddCommand(tlsProxyCmd)
}
