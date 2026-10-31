package netinfo

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Dial: принимает ли адрес соединения и сколько заняло рукопожатие.
func Dial(addr string, limit time.Duration) (time.Duration, error) {
	start := time.Now()
	c, err := net.DialTimeout("tcp", addr, limit)
	if err != nil {
		return 0, err
	}
	_ = c.Close()
	return time.Since(start), nil
}

// ExternalIP: адрес, которым машина видна в интернете; proxy - через SOCKS или HTTP-прокси, пусто - напрямую.
func ExternalIP(proxy string) (string, error) {
	tr := &http.Transport{}
	if proxy != "" {
		u, err := url.Parse(proxy)
		if err != nil {
			return "", err
		}
		tr.Proxy = http.ProxyURL(u)
	}
	client := &http.Client{Timeout: 6 * time.Second, Transport: tr}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64))
	return strings.TrimSpace(string(b)), err
}
