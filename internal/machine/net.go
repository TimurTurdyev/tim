package machine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// warning: шаг не прошёл, но это не обязательно поломка.
type warning string

func (w warning) Error() string { return string(w) }

// step: шаг проверки сети; ошибка warning - предупреждение, остальные - поломка.
type step struct {
	name string
	run  func() (string, error)
}

// checkNet проходит сеть по шагам и называет первый упавший.
func checkNet() error {
	failed := ""
	for _, s := range netSteps() {
		res, err := s.run()
		if w, ok := errors.AsType[warning](err); ok {
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
}

func netSteps() []step {
	phys, gw := netinfo.Physical()
	return []step{
		{"интерфейс", func() (string, error) {
			if phys.Device == "" {
				return "", fmt.Errorf("активного нет: Wi-Fi или кабель выключены")
			}
			return phys.Device + " (" + phys.Name + "), шлюз " + ui.Dash(gw), nil
		}},
		{"шлюз", func() (string, error) { return pingGateway(gw) }},
		{"интернет", func() (string, error) {
			took, err := netinfo.Dial("1.1.1.1:443", 3*time.Second)
			if err != nil {
				return "", fmt.Errorf("1.1.1.1:443 недоступен: %w", err)
			}
			return fmt.Sprintf("1.1.1.1:443 за %d мс", took.Milliseconds()), nil
		}},
		{"dns", resolveProbe},
		{"внешний ip", func() (string, error) {
			ip, err := netinfo.ExternalIP("")
			if err != nil {
				return "", fmt.Errorf("не узнать: %w", err)
			}
			return ip, nil
		}},
		{"маршрут", func() (string, error) {
			inet := netinfo.InternetIface()
			if netinfo.IsTunnelIface(inet) {
				return "через туннель " + inet + " (VPN)", nil
			}
			return "напрямую через " + ui.Dash(inet), nil
		}},
	}
}

func pingGateway(gw string) (string, error) {
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
}

func resolveProbe() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	ips, err := net.DefaultResolver.LookupHost(ctx, "example.com")
	if err != nil {
		return "", fmt.Errorf("example.com не резолвится: %w", err)
	}
	servers := strings.Join(netinfo.DNSServers(), " ")
	return fmt.Sprintf("example.com -> %s за %d мс, серверы %s", ips[0], time.Since(start).Milliseconds(), ui.Dash(servers)), nil
}

func printNetTop(seconds, count int) error {
	if runtime.GOOS != "darwin" {
		return ui.Errorf("пока только macOS (nettop); на Linux - nethogs")
	}
	d := time.Duration(seconds) * time.Second
	out, err := runner.OutputWithin(0, "nettop", "-P", "-L", "2", "-d", "-s", strconv.Itoa(seconds), "-x", "-J", "bytes_in,bytes_out")
	if err != nil {
		return ui.Errorf("nettop: %v", err)
	}
	flows := netinfo.ParseNettop(out)
	if len(flows) == 0 {
		ui.Println(ui.Dim("за " + strconv.Itoa(seconds) + " с сеть никто не трогал"))
		return nil
	}
	rate := func(bytes int64) string { return ui.Bytes(int64(float64(bytes)/d.Seconds())) + "/с" }
	ui.Println(ui.Dim(ui.Pad("процесс", 30) + ui.Pad("приём", 14) + "отдача"))
	for i, f := range flows {
		if i == count {
			break
		}
		name := procs.AppName(f.Name)
		ui.Println(ui.Label(ui.Pad(ui.Clip(name, 29), 30)) + ui.Pad(rate(f.In), 14) + rate(f.Out) + ui.Dim("  pid "+f.PID))
	}
	return nil
}

func printWifi() error {
	if runtime.GOOS == "linux" {
		printLinuxWifi()
		return nil
	}
	out, err := runner.Output("system_profiler", "SPAirPortDataType", "-json")
	if err != nil {
		return ui.Errorf("system_profiler: %v", err)
	}
	w, ok := sysinfo.ParseWifi([]byte(out))
	if !ok {
		ui.Println(ui.Dim("к Wi-Fi не подключено"))
		return nil
	}
	ssid := w.SSID
	if ssid == "<redacted>" {
		ssid = ui.Dim("имя скрыто macOS: дай терминалу доступ к геолокации (Настройки - Конфиденциальность - Службы геолокации)")
	}
	ui.Println(ui.Label("сеть     ") + ssid + ui.Dim("  "+w.Iface))
	ui.Println(ui.Label("канал    ") + w.Channel)
	ui.Println(ui.Label("стандарт ") + w.PHY + ui.Dim("  "+strconv.Itoa(w.Rate)+" Мбит/с"))
	q := w.Quality()
	sig := strconv.Itoa(w.Signal) + " dBm, шум " + strconv.Itoa(w.Noise) + " dBm  " + q
	if q == "слабый" || q == "плохой" {
		sig = ui.Warn(sig)
	}
	ui.Println(ui.Label("сигнал   ") + sig)
	ui.Println(ui.Label("защита   ") + w.Security)
	return nil
}

// printLinuxWifi: через NetworkManager; сигнал там в процентах, а не в dBm.
func printLinuxWifi() {
	if !runner.Has("nmcli") {
		ui.Println(ui.Dim("nmcli не найден: Wi-Fi смотреть нечем (на сервере его обычно и нет)"))
		return
	}
	out, _ := runner.Output("nmcli", "-t", "-f", "ACTIVE,SSID,CHAN,RATE,SIGNAL,SECURITY", "dev", "wifi")
	w, ok := sysinfo.ParseNmcliWifi(out)
	if !ok {
		ui.Println(ui.Dim("к Wi-Fi не подключено"))
		return
	}
	ui.Println(ui.Label("сеть     ") + w.SSID)
	ui.Println(ui.Label("канал    ") + w.Channel + ui.Dim("  "+strconv.Itoa(w.Rate)+" Мбит/с"))
	ui.Println(ui.Label("сигнал   ") + strconv.Itoa(w.Signal) + "%")
	ui.Println(ui.Label("защита   ") + w.Security)
}
