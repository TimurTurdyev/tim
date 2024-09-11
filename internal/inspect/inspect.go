// Package inspect: проверки сертификата, DNS, портов, регистрации домена и сайта; только чтение по сети.
package inspect

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

const Timeout = 5 * time.Second

type CertInfo struct {
	Version   string
	Chain     []*x509.Certificate
	VerifyErr error
}

func (c *CertInfo) Leaf() *x509.Certificate { return c.Chain[0] }

// Cert снимает сертификат; при ошибке проверки цепочки повторяет без проверки, чтобы показать, что там.
func Cert(host, port string) (*CertInfo, error) {
	addr := net.JoinHostPort(host, port)
	dialer := &net.Dialer{Timeout: Timeout}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host})
	var verifyErr error
	if err != nil {
		var cv *tls.CertificateVerificationError
		if !errors.As(err, &cv) {
			return nil, err
		}
		verifyErr = err
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host, InsecureSkipVerify: true})
		if err != nil {
			return nil, err
		}
	}
	defer func() { _ = conn.Close() }()
	st := conn.ConnectionState()
	return &CertInfo{Version: tls.VersionName(st.Version), Chain: st.PeerCertificates, VerifyErr: verifyErr}, nil
}

type Record struct {
	Type  string
	Value string
}

// DNS собирает A, AAAA, CNAME, MX, NS, TXT; server - "1.1.1.1" или пусто для системного резолвера.
func DNS(host, server string) []Record {
	r := net.DefaultResolver
	if server != "" {
		if !strings.Contains(server, ":") {
			server += ":53"
		}
		r = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: Timeout}).DialContext(ctx, network, server)
		}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	var out []Record
	if ips, err := r.LookupIPAddr(ctx, host); err == nil {
		for _, ip := range ips {
			t := "A"
			if ip.IP.To4() == nil {
				t = "AAAA"
			}
			out = append(out, Record{t, ip.IP.String()})
		}
	}
	if cn, err := r.LookupCNAME(ctx, host); err == nil && strings.TrimSuffix(cn, ".") != strings.TrimSuffix(host, ".") {
		out = append(out, Record{"CNAME", strings.TrimSuffix(cn, ".")})
	}
	if mx, err := r.LookupMX(ctx, host); err == nil {
		for _, m := range mx {
			h := strings.TrimSuffix(m.Host, ".")
			if h == "" {
				out = append(out, Record{"MX", "почты нет (null MX)"})
				continue
			}
			out = append(out, Record{"MX", fmt.Sprintf("%s %d", h, m.Pref)})
		}
	}
	if ns, err := r.LookupNS(ctx, host); err == nil {
		for _, n := range ns {
			out = append(out, Record{"NS", strings.TrimSuffix(n.Host, ".")})
		}
	}
	if txt, err := r.LookupTXT(ctx, host); err == nil {
		for _, t := range txt {
			out = append(out, Record{"TXT", t})
		}
	}
	order := map[string]int{"A": 0, "AAAA": 1, "CNAME": 2, "MX": 3, "NS": 4, "TXT": 5}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i].Type] < order[out[j].Type] })
	return out
}

// Dial проверяет TCP-порт и меряет время соединения.
func Dial(addr string) (time.Duration, error) {
	start := time.Now()
	c, err := net.DialTimeout("tcp", addr, Timeout)
	if err != nil {
		return 0, err
	}
	_ = c.Close()
	return time.Since(start), nil
}
