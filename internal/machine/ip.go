package machine

import (
	"fmt"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
)

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
