package inspect

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"
)

// Timing: фазы одного запроса на новом соединении.
type Timing struct {
	URL                          string
	Status                       int
	Proto                        string
	Size                         int64
	DNS, Connect, TLS, TTFB, All time.Duration
	Header                       http.Header
	Location                     string
}

// Measure делает один GET без повторного использования соединений и без автоматических редиректов.
func Measure(url string) (*Timing, error) {
	var dnsStart, connStart, tlsStart, start time.Time
	t := &Timing{URL: url}
	trace := &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { dnsStart = time.Now() },
		DNSDone:              func(httptrace.DNSDoneInfo) { t.DNS = time.Since(dnsStart) },
		ConnectStart:         func(string, string) { connStart = time.Now() },
		ConnectDone:          func(string, string, error) { t.Connect = time.Since(connStart) },
		TLSHandshakeStart:    func() { tlsStart = time.Now() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { t.TLS = time.Since(tlsStart) },
		GotFirstResponseByte: func() { t.TTFB = time.Since(start) },
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) tim")
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	client := &http.Client{
		Timeout:       30 * time.Second,
		Transport:     &http.Transport{DisableKeepAlives: true, ForceAttemptHTTP2: true, Proxy: http.ProxyFromEnvironment},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	start = time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	t.Size, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 50<<20))
	t.All = time.Since(start)
	t.Status, t.Proto, t.Header = resp.StatusCode, resp.Proto, resp.Header
	if loc, err := resp.Location(); err == nil {
		t.Location = loc.String()
	}
	return t, nil
}
