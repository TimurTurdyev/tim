package internetdb

import (
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	got, err := Parse([]byte(`{"ip":"192.0.2.1","ports":[443,22,80],"tags":["cloud"],"hostnames":["mail.example.com"],
		"cpes":["cpe:/a:f5:nginx","cpe:/a:apache:http_server:2.4.39","cpe:/a:f5:nginx"],"vulns":["CVE-2024-2","CVE-2019-1"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Ports, []int{22, 80, 443}) || !slices.Equal(got.Products, []string{"nginx", "Apache 2.4.39"}) ||
		!slices.Equal(got.CVE, []string{"CVE-2019-1", "CVE-2024-2"}) || !slices.Equal(got.Names, []string{"mail.example.com"}) {
		t.Errorf("%+v", got)
	}
	if got.CDN() || got.Empty() {
		t.Error("адрес с портами принят за CDN или пустой")
	}
	if cdn, _ := Parse([]byte(`{"ports":[80],"tags":["cdn"]}`)); !cdn.CDN() {
		t.Error("метка cdn не замечена")
	}
	if _, err := Parse([]byte("<html>")); err == nil {
		t.Error("не JSON принят")
	}
}

func TestProduct(t *testing.T) {
	cases := map[string]string{
		"cpe:/a:f5:nginx":                  "nginx",
		"cpe:/a:openbsd:openssh:6.6.1p1":   "OpenSSH 6.6.1p1",
		"cpe:/o:canonical:ubuntu_linux":    "Ubuntu",
		"cpe:/a:getbootstrap:bootstrap::~": "Bootstrap",
		"cpe:2.3:a:1c:bitrix24:22.0.300:*": "Bitrix24 22.0.300",
		"cpe:2.3:a:vendor:some_tool:*:*":   "some tool",
		"cpe:/a:x:c%2b%2b_lib:1.0":         "c++ lib 1.0",
		"cpe:/a:x":                         "",
		"nginx":                            "",
	}
	for in, want := range cases {
		if got := Product(in); got != want {
			t.Errorf("%s: %q, ждали %q", in, got, want)
		}
	}
}
