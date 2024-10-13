package localca

import (
	"crypto/x509"
	"net"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	dns, ips := Names([]string{"shop.test", "127.0.0.1", "*.api.test", "localhost", "shop.test.", "::1"}, false)
	if got := strings.Join(dns, " "); got != "shop.test *.shop.test *.api.test localhost" {
		t.Fatalf("имена: %q", got)
	}
	if len(ips) != 2 || !ips[0].Equal(net.IPv4(127, 0, 0, 1)) {
		t.Fatalf("адреса: %v", ips)
	}
	// localhost без точки wildcard не получает, а повтор shop.test. не дублируется.
	exact, _ := Names([]string{"shop.test"}, true)
	if got := strings.Join(exact, " "); got != "shop.test" {
		t.Fatalf("--exact: %q", got)
	}
}

func TestIssueVerifies(t *testing.T) {
	t.Setenv("TIM_CAROOT", t.TempDir())
	ca, fresh, err := LoadOrCreate()
	if err != nil || !fresh {
		t.Fatalf("CA не создан: %v %v", fresh, err)
	}
	again, fresh, err := LoadOrCreate()
	if err != nil || fresh {
		t.Fatalf("CA должен был прочитаться, а не создаться заново: %v %v", fresh, err)
	}
	if !again.Cert.Equal(ca.Cert) {
		t.Fatal("прочитан другой CA")
	}
	dns, ips := Names([]string{"shop.test", "127.0.0.1"}, false)
	leaf, err := ca.Issue(dns, ips, 30)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := parseCert(leaf.Cert)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.Cert)
	for _, name := range []string{"shop.test", "a.shop.test"} {
		if _, err := cert.Verify(x509.VerifyOptions{DNSName: name, Roots: pool}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := cert.Verify(x509.VerifyOptions{DNSName: "other.test", Roots: pool}); err == nil {
		t.Error("чужое имя не должно проходить проверку")
	}
	if _, err := ca.Issue(nil, nil, 30); err == nil {
		t.Error("сертификат без имён выпускать нельзя")
	}
}
