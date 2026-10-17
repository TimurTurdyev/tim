package audit

import (
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/health"
	"github.com/TimurTurdyev/tim/internal/level"
)

const tlsTimeout = 5 * time.Second

// oldVersions: версии TLS, которые сервер принимать не должен.
var oldVersions = []struct {
	id   uint16
	name string
}{{tls.VersionTLS10, "TLS 1.0"}, {tls.VersionTLS11, "TLS 1.1"}}

// TLSFacts: что сервер принимает и отдаёт по TLS.
type TLSFacts struct {
	Old     []string  `json:"old,omitempty"`
	Invalid string    `json:"invalid,omitempty"`
	NoChain bool      `json:"no_chain,omitempty"`
	RSABits int       `json:"rsa_bits,omitempty"`
	Expires time.Time `json:"expires"`
}

// TLSFindings: решения по фактам TLS.
func TLSFindings(t TLSFacts, now time.Time) []Finding {
	var out []Finding
	add := func(l level.Level, text, why, fix string) {
		out = append(out, Finding{Area: "tls", Level: l, Text: text, Why: why, Fix: fix})
	}
	if t.Invalid != "" {
		add(level.Bad, "сертификат не проходит проверку: "+t.Invalid, "браузер покажет предупреждение вместо сайта",
			"выпустить заново: certbot --nginx -d <домен>")
	} else if d := health.DeadlineAt("сертификат", t.Expires, now, health.CertWarnDays, health.CertBadDays); !t.Expires.IsZero() && d.Level > level.OK {
		add(d.Level, d.Detail, "после срока браузер не откроет сайт", "certbot renew, проверить таймер: systemctl status certbot.timer")
	}
	if t.NoChain {
		add(level.Warn, "сервер не отдаёт промежуточный сертификат",
			"браузеры достроят цепочку сами, а curl, вебхуки, платёжные шлюзы и старые Android - нет",
			"ssl_certificate - fullchain.pem, а не cert.pem")
	}
	if len(t.Old) > 0 {
		add(level.Warn, "принимает "+strings.Join(t.Old, " и "), "старые версии TLS уязвимы, браузеры их давно не используют",
			"в nginx: ssl_protocols TLSv1.2 TLSv1.3;")
	}
	if t.RSABits > 0 && t.RSABits < 2048 {
		add(level.Bad, fmt.Sprintf("ключ RSA %d бит", t.RSABits), "такой ключ подбирается", "новый ключ от 2048 бит или ECDSA")
	}
	return out
}

// collectTLS: одно рукопожатие без проверки - чтобы увидеть сертификаты даже у сломанной цепочки, и по
// одному на каждую устаревшую версию.
func collectTLS(host string) TLSFacts {
	var t TLSFacts
	dialer := &net.Dialer{Timeout: tlsTimeout}
	addr := net.JoinHostPort(host, "443")
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: host, InsecureSkipVerify: true})
	if err != nil {
		return t
	}
	certs := conn.ConnectionState().PeerCertificates
	_ = conn.Close()
	if len(certs) == 0 {
		return t
	}
	leaf := certs[0]
	t.Expires = leaf.NotAfter
	if k, ok := leaf.PublicKey.(*rsa.PublicKey); ok {
		t.RSABits = k.N.BitLen()
	}
	// macOS при проверке сам докачивает промежуточный сертификат, поэтому его отсутствие видно только по счёту.
	t.NoChain = len(certs) == 1 && leaf.Issuer.String() != leaf.Subject.String()
	pool := x509.NewCertPool()
	for _, c := range certs[1:] {
		pool.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: host, Intermediates: pool}); err != nil && !t.NoChain {
		t.Invalid = invalidReason(err)
	}
	suites := allSuites()
	for _, v := range oldVersions {
		cfg := &tls.Config{ServerName: host, InsecureSkipVerify: true, MinVersion: v.id, MaxVersion: v.id, CipherSuites: suites}
		if c, err := tls.DialWithDialer(dialer, "tcp", addr, cfg); err == nil {
			_ = c.Close()
			t.Old = append(t.Old, v.name)
		}
	}
	return t
}

func invalidReason(err error) string {
	var inv x509.CertificateInvalidError
	var host x509.HostnameError
	var unknown x509.UnknownAuthorityError
	switch {
	case errors.As(err, &inv) && inv.Reason == x509.Expired:
		return "истёк"
	case errors.As(err, &host):
		return "выдан не на этот домен"
	case errors.As(err, &unknown):
		return "выдан неизвестным центром, похоже на самоподписанный"
	}
	return err.Error()
}

// allSuites: и небезопасные наборы - старые версии TLS без них не договорятся.
func allSuites() []uint16 {
	var ids []uint16
	for _, s := range append(tls.CipherSuites(), tls.InsecureCipherSuites()...) {
		ids = append(ids, s.ID)
	}
	return ids
}
