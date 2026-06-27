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

// WaitPort ждёт, пока локальный порт начнёт принимать соединения; false - не дождались за limit.
func WaitPort(port string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if _, err := Dial("127.0.0.1:"+port, 300*time.Millisecond); err == nil {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// FreePort: свободный порт на loopback, который выдала система.
func FreePort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer func() { _ = l.Close() }()
	_, port, err := net.SplitHostPort(l.Addr().String())
	return port, err
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

// IfaceIPv4: первый IPv4-адрес интерфейса, пусто - адреса нет.
func IfaceIPv4(dev string) string {
	ifi, err := net.InterfaceByName(dev)
	if err != nil {
		return ""
	}
	addrs, _ := ifi.Addrs()
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
			return ipn.IP.To4().String()
		}
	}
	return ""
}
