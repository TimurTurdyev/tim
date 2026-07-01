package bypass

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/tproxy"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// proxyOptions: где слушать прозрачному прокси и через какой интерфейс ходить.
type proxyOptions struct {
	port      int
	iface, ip string
}

// serveProxy: прозрачный прокси для обхода по приложению, живёт под root отдельно от tim.
func serveProxy(o proxyOptions) error {
	if os.Geteuid() != 0 {
		return ui.Errorf("novpn-proxy запускает tim vpn app под root, руками он не нужен")
	}
	ip := net.ParseIP(o.ip)
	if ip == nil || o.iface == "" {
		return ui.Errorf("нужны --iface и --ip")
	}
	if err := runner.WriteAtomic(ProxyPidFile(), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		return err
	}
	defer func() { _ = os.Remove(ProxyPidFile()) }()
	logFile, err := os.OpenFile(ProxyLogFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = logFile.Close() }()
	logf := func(format string, a ...any) {
		_, _ = fmt.Fprintf(logFile, time.Now().Format("2006-01-02 15:04:05")+" "+format+"\n", a...)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	logf("старт: 127.0.0.1:%d через %s %s", o.port, o.iface, o.ip)
	err = tproxy.Serve(ctx, tproxy.Options{Listen: "127.0.0.1:" + strconv.Itoa(o.port), Iface: o.iface, Local: ip, Logf: logf})
	logf("стоп: %v", err)
	return err
}
