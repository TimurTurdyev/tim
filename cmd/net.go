package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var netCmd = &cobra.Command{
	Use:   "net",
	Short: "Сеть: проверка по шагам",
}

// warning: шаг не прошёл, но это не обязательно поломка.
type warning string

func (w warning) Error() string { return string(w) }

type step struct {
	name string
	run  func() (string, error)
}

var netCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Шлюз, интернет, DNS, внешний IP, VPN: что именно не работает",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		phys, gw := physical()
		steps := []step{
			{"интерфейс", func() (string, error) {
				if phys.Device == "" {
					return "", fmt.Errorf("активного нет: Wi-Fi или кабель выключены")
				}
				return phys.Device + " (" + phys.Name + "), шлюз " + ui.Dash(gw), nil
			}},
			{"шлюз", func() (string, error) {
				if gw == "" {
					return "", fmt.Errorf("шлюза нет: DHCP не выдал адрес?")
				}
				// Таймаут ответа: -t на macOS, -W на Linux (там -t - это TTL).
				wait := "-t2"
				if runtime.GOOS == "linux" {
					wait = "-W2"
				}
				if _, err := runner.Output("ping", "-c1", wait, gw); err != nil {
					return "", warning(gw + " не отвечает на ping: роутер или VPN могут глушить ICMP, смотри следующие шаги")
				}
				return gw + " отвечает", nil
			}},
			{"интернет", func() (string, error) { return dialTimed("1.1.1.1:443") }},
			{"dns", func() (string, error) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				start := time.Now()
				ips, err := net.DefaultResolver.LookupHost(ctx, "example.com")
				if err != nil {
					return "", fmt.Errorf("example.com не резолвится: %v", err)
				}
				servers := strings.Join(netinfo.DNSServers(), " ")
				return fmt.Sprintf("example.com -> %s за %d мс, серверы %s", ips[0], time.Since(start).Milliseconds(), ui.Dash(servers)), nil
			}},
			{"внешний ip", externalIP},
			{"маршрут", func() (string, error) {
				inet := netinfo.InternetIface()
				if netinfo.IsTunnelIface(inet) {
					return "через туннель " + inet + " (VPN)", nil
				}
				return "напрямую через " + ui.Dash(inet), nil
			}},
		}
		failed := ""
		for _, s := range steps {
			res, err := s.run()
			if w, ok := err.(warning); ok {
				ui.Println(ui.Warn(ui.Pad(s.name, 11)) + string(w))
				continue
			}
			if err != nil {
				ui.Println(ui.Bad(ui.Pad(s.name, 11)) + err.Error())
				if failed == "" {
					failed = s.name
				}
				continue
			}
			ui.Println(ui.Label(ui.Pad(s.name, 11)) + res)
		}
		if failed != "" {
			return ui.Errorf("первым упал шаг %q - начни с него", failed)
		}
		return nil
	},
}

func dialTimed(addr string) (string, error) {
	start := time.Now()
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return "", fmt.Errorf("%s недоступен: %v", addr, err)
	}
	_ = c.Close()
	return fmt.Sprintf("%s за %d мс", addr, time.Since(start).Milliseconds()), nil
}

func externalIP() (string, error) {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", fmt.Errorf("не узнать: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func init() {
	netCmd.AddCommand(netCheckCmd)
	root.AddCommand(netCmd)
}
