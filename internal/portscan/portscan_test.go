package portscan

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func byPort(port int) probe {
	for _, p := range probes {
		if p.port == port {
			return p
		}
	}
	return probe{port: port, name: "http"}
}

// Кто ответил - по самому ответу; чужой ответ на порту не засчитывается как его сервис.
func TestClassify(t *testing.T) {
	cases := []struct {
		port    int
		reply   string
		name    string // пусто - не узнали
		product string
	}{
		{22, "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.5\r\n", "ssh", "OpenSSH 9.6p1 Ubuntu"},
		{22, "SSH-2.0-dropbear_2022.83\r\n", "ssh", "dropbear 2022.83"},
		{25, "220 mx.example.com ESMTP Postfix (Ubuntu)\r\n", "smtp", "Postfix"},
		{25, "554 5.7.1 no\r\n", "smtp", ""},
		{21, "220 ProFTPD 1.3.8b Server ready.\r\n", "ftp", "ProFTPD 1.3.8b"},
		{110, "+OK Dovecot (Ubuntu) ready.\r\n", "pop3", "Dovecot"},
		{143, "* OK [CAPABILITY IMAP4rev1] Dovecot ready.\r\n", "imap", "Dovecot"},
		{80, "HTTP/1.1 301 Moved\r\nServer: nginx/1.24.0 (Ubuntu)\r\n\r\n", "http", "nginx/1.24.0 (Ubuntu)"},
		{9200, "HTTP/1.1 200 OK\r\n\r\n{\"cluster_name\" : \"prod\"}", "elasticsearch", ""},
		{2375, "HTTP/1.1 200 OK\r\nServer: Docker/27.0\r\n\r\n{\"ApiVersion\":\"1.46\"}", "docker", "Docker/27.0"},
		{27017, "HTTP/1.0 200 OK\r\n\r\nIt looks like you are trying to access MongoDB over HTTP on the native driver port.", "mongodb", ""},
		{3306, "\x4a\x00\x00\x00\x0a8.0.36-0ubuntu0.22.04.1\x00rest", "mysql", "MySQL 8.0.36"},
		{3306, "\x5a\x00\x00\x00\x0a5.5.5-10.6.16-MariaDB-0ubuntu0.22.04.1\x00", "mysql", "MariaDB 10.6.16"},
		{3306, "\x45\x00\x00\x00\xffj\x04Host is not allowed", "mysql", ""},
		{5432, "N", "postgresql", ""},
		{6379, "-NOAUTH Authentication required.\r\n", "redis", ""},
		{11211, "VERSION 1.6.21\r\n", "memcached", "1.6.21"},
		{5900, "RFB 003.008\n", "vnc", ""},
		{3389, "\x03\x00\x00\x13\x0e\xd0\x00\x00\x124\x00", "rdp", ""},
		{53, "\x00\x1f\x74\x6d\x81\x80", "dns", ""},
		{3306, "HTTP/1.1 400 Bad Request\r\n\r\n", "http", ""},
		{6379, "HTTP/1.1 400 Bad Request\r\nServer: nginx\r\n\r\n", "http", "nginx"},
		{5432, "<html>", "", ""},
		{6379, "", "", ""},
		{22, "\x00\x00", "", ""},
	}
	for _, c := range cases {
		name, product, ok := classify(byPort(c.port), []byte(c.reply))
		if ok != (c.name != "") || name != c.name || product != c.product {
			t.Errorf("%d %q: %q %q %v, ждали %q %q", c.port, c.reply, name, product, ok, c.name, c.product)
		}
	}
}

// listen: сервис на свободном порту; hello - что сказать сразу, пусто - принять и молчать.
func listen(t *testing.T, hello string) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte(hello))
			if hello == "" {
				time.Sleep(time.Second)
			}
			_ = c.Close()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

func port(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	n, _ := strconv.Atoi(p)
	return n
}

// Узнаётся тот, кто ответил, в том числе HTTP за TLS на чужом порту; молчун - только если сеть не принимает всё подряд.
func TestLook(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "box.example.com"}, NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	secure := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Header().Set("Server", "Angie") }))
	secure.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	secure.StartTLS()
	defer secure.Close()
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"cluster_name":"x"}`)) }))
	defer plain.Close()
	ssh, quiet := listen(t, "SSH-2.0-OpenSSH_9.6\r\n"), listen(t, "")
	gone, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := port(gone.Addr().String())
	_ = gone.Close()

	cases := []struct {
		what        string
		p           probe
		intercepted bool
		want        string
	}{
		{"ssh", probe{port: ssh, name: "ssh", greets: true}, true, "ssh OpenSSH 9.6"},
		{"HTTP за TLS на порту без TLS", probe{port: port(secure.Listener.Addr().String()), name: "http"}, true, "http"},
		{"TLS", probe{port: port(secure.Listener.Addr().String()), name: "https", tls: true}, true, "https Angie box.example.com"},
		{"HTTP на порту TLS", probe{port: port(plain.Listener.Addr().String()), name: "https", tls: true}, true, "elasticsearch"},
		{"молчит, сеть честная", probe{port: quiet, name: "smtp", greets: true}, false, "smtp молчит"},
		{"молчит, сеть принимает всё", probe{port: quiet, name: "smtp", greets: true}, true, ""},
		{"закрыт", probe{port: closed, name: "ssh", greets: true}, false, ""},
	}
	for _, c := range cases {
		got := ""
		if s := look(c.p, "127.0.0.1", "box.example.com", c.intercepted); s != nil {
			got = s.Name
			for _, part := range []string{s.Product, s.Cert} {
				if part != "" {
					got += " " + part
				}
			}
			if s.Silent {
				got += " молчит"
			}
		}
		if got != c.want {
			t.Errorf("%s: %q, ждали %q", c.what, got, c.want)
		}
	}
}
