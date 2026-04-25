package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/tproxy"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// tim novpn-proxy: прозрачный прокси для tim vpn app, живёт под root отдельно от tim.

var (
	proxyPort  int
	proxyIface string
	proxyIP    string
)

var novpnProxyCmd = &cobra.Command{
	Use:    "novpn-proxy",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if os.Geteuid() != 0 {
			return ui.Errorf("novpn-proxy запускает tim vpn app под root, руками он не нужен")
		}
		ip := net.ParseIP(proxyIP)
		if ip == nil || proxyIface == "" {
			return ui.Errorf("нужны --iface и --ip")
		}
		_ = os.MkdirAll(bypass.GuardDir(), 0o755)
		if err := os.WriteFile(bypass.ProxyPidFile(), []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
			return err
		}
		defer func() { _ = os.Remove(bypass.ProxyPidFile()) }()
		logFile, err := os.OpenFile(bypass.ProxyLogFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		defer func() { _ = logFile.Close() }()
		logf := func(format string, a ...any) {
			_, _ = fmt.Fprintf(logFile, time.Now().Format("2006-01-02 15:04:05")+" "+format+"\n", a...)
		}
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		logf("старт: 127.0.0.1:%d через %s %s", proxyPort, proxyIface, proxyIP)
		err = tproxy.Serve(ctx, tproxy.Options{Listen: "127.0.0.1:" + strconv.Itoa(proxyPort), Iface: proxyIface, Local: ip, Logf: logf})
		logf("стоп: %v", err)
		return err
	},
}

func init() {
	novpnProxyCmd.Flags().IntVar(&proxyPort, "port", bypass.ProxyPort, "порт на 127.0.0.1")
	novpnProxyCmd.Flags().StringVar(&proxyIface, "iface", "", "физический интерфейс")
	novpnProxyCmd.Flags().StringVar(&proxyIP, "ip", "", "его адрес")
	root.AddCommand(novpnProxyCmd)
}
