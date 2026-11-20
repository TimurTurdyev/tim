package dnsq

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// dohServers: DNS поверх HTTPS - его не перехватывает VPN, который отвечает за любой адрес на порту 53.
var dohServers = []string{"https://cloudflare-dns.com/dns-query", "https://dns.google/dns-query"}

var dohResolver = relayed(overHTTPS)

func overHTTPS(ctx context.Context, msg []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var last error
	for _, u := range dohServers {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(msg))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/dns-message")
		req.Header.Set("Accept", "application/dns-message")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			last = err
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		if err == nil && resp.StatusCode != http.StatusOK {
			err = errors.New("DoH " + u + ": " + resp.Status)
		}
		if err != nil {
			last = err
			continue
		}
		return body, nil
	}
	return nil, last
}
