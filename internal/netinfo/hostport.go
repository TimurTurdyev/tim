package netinfo

import (
	"net"
	"net/url"
	"strings"
)

// HostPort: из "host", "host:port" или url достаёт хост и порт.
func HostPort(s, defPort string) (string, string) {
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		s = u.Host
	}
	if h, p, err := net.SplitHostPort(s); err == nil {
		return h, p
	}
	return strings.TrimSuffix(s, "/"), defPort
}
