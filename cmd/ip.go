package cmd

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

func geoLine(g *inspect.Geo) string {
	return fmt.Sprintf("%s  %s, %s  %s AS%d", g.IP, g.Country, g.City, g.ISP, g.ASN)
}

// geoVia: IP и геолокация через SOCKS-прокси.
func geoVia(proxy string) (*inspect.Geo, error) {
	ip, err := ipVia(proxy)
	if err != nil {
		return nil, err
	}
	return inspect.IPGeo(ip)
}

var ipCmd = &cobra.Command{
	Use:   "ip",
	Short: "Внешний IP: страна, город, провайдер; напрямую и через SOCKS tim",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		inet := netinfo.InternetIface()
		path := "напрямую через " + ui.Dash(inet)
		if netinfo.IsTunnelIface(inet) {
			path = "через туннель " + inet + " (VPN)"
		}
		if _, ok := shuttlePID(); ok {
			path += ", tunnel all"
		}
		ip, err := externalIP()
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
			proxy := "socks5://127.0.0.1:" + strconv.Itoa(socksPort())
			if g, err := geoVia(proxy); err == nil {
				ui.Println(ui.Label("через SOCKS ") + geoLine(g))
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if names, err := net.DefaultResolver.LookupAddr(ctx, ip); err == nil && len(names) > 0 {
			ui.Println(ui.Dim("           обратная запись " + names[0]))
		}
		return nil
	},
}

func init() { root.AddCommand(ipCmd) }
