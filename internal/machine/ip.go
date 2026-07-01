package machine

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// printIP: внешний адрес с геолокацией, путь до интернета и тот же адрес через SOCKS tim.
func printIP(socksPort int) error {
	inet := netinfo.InternetIface()
	path := "напрямую через " + ui.Dash(inet)
	if netinfo.IsTunnelIface(inet) {
		path = "через туннель " + inet + " (VPN)"
	}
	if _, ok := sshx.ShuttlePID(); ok {
		path += ", tunnel all"
	}
	ip, err := netinfo.ExternalIP("")
	if err != nil {
		return ui.Errorf("внешний IP не узнать: %v", err)
	}
	if g, err := inspect.IPGeo(ip); err == nil {
		ui.Println(ui.Label("снаружи    ") + geoLine(g))
	} else {
		ui.Println(ui.Label("снаружи    ") + ip + ui.Dim("  геолокация: "+err.Error()))
	}
	ui.Println(ui.Dim("           " + path))
	for _, t := range sshx.Known() {
		if t.Name != "socks" || !t.Alive() {
			continue
		}
		if g, err := geoVia("socks5://127.0.0.1:" + strconv.Itoa(socksPort)); err == nil {
			ui.Println(ui.Label("через SOCKS ") + geoLine(g))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if names, err := net.DefaultResolver.LookupAddr(ctx, ip); err == nil && len(names) > 0 {
		ui.Println(ui.Dim("           обратная запись " + names[0]))
	}
	return nil
}

func geoLine(g *inspect.Geo) string {
	return fmt.Sprintf("%s  %s, %s  %s AS%d", g.IP, g.Country, g.City, g.ISP, g.ASN)
}

// geoVia: IP и геолокация через прокси.
func geoVia(proxy string) (*inspect.Geo, error) {
	ip, err := netinfo.ExternalIP(proxy)
	if err != nil {
		return nil, err
	}
	return inspect.IPGeo(ip)
}
