package harvest

import (
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"
)

func sample(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// equal сверяет списки вместе с порядком и называет разницу.
func equal(t *testing.T, what string, got, want []string) {
	t.Helper()
	if slices.Equal(got, want) {
		return
	}
	extra := slices.DeleteFunc(slices.Clone(got), func(s string) bool { return slices.Contains(want, s) })
	missing := slices.DeleteFunc(slices.Clone(want), func(s string) bool { return slices.Contains(got, s) })
	t.Errorf("%s: лишние %v, не хватает %v, получено %v", what, extra, missing, got)
}

func sorted(names ...[]string) []string {
	all := slices.Concat(names...)
	slices.Sort(all)
	return slices.Compact(all)
}

func TestPageNames(t *testing.T) {
	header := http.Header{}
	for line := range strings.SplitSeq(sample(t, "headers.txt"), "\n") {
		if key, value, ok := strings.Cut(line, ": "); ok {
			header.Add(key, value)
		}
	}
	body := sample(t, "page.html")
	fromBody := strings.Fields(`example.com www.example.com de.example.com static.example.com img2.example.com
		cdn.example.com status.example.com media.example.com assets.example.com legacy-auth.example.com js.example.com
		api.example.com ws.example.com sso.example.com metrics.internal.example.com a1.cdn.example.com
		a2.cdn.example.com pay.example.com account.example.com partners.example.com blog.example.com forum.example.com
		files.example.com helpdesk.example.com shop.example.com promo.example.com img.example.com img3.example.com
		video.example.com stream.example.com pay-test.example.com widgets.example.com graphql.example.com
		eu.edge.example.com us.edge.example.com ftp.example.com rsync.example.com xn--e1afmkfd.example.com
		apps.example.com _dmarc.example.com dash.example.com`)
	fromHeader := strings.Fields(`www.example.com m.example.com static.example.com cdn.example.com js.example.com
		ws.example.com api.example.com sentry.internal.example.com partners.example.com csp-report.example.com
		img.example.com jira.corp.example.com fonts.example.com edge.example.com example.com sso.example.com
		app.example.com nel.example.com cache-fra-01.example.com`)

	equal(t, "тело", PageNames("example.com", body, nil), sorted(fromBody))
	equal(t, "заголовки", PageNames("example.com", "", header), sorted(fromHeader))
	equal(t, "вместе", PageNames("Example.COM.", body, header), sorted(fromBody, fromHeader))
}

func TestPageNamesEdges(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"example.com", []string{"example.com"}},
		{"x.EXAMPLE.com.", []string{"x.example.com"}},
		{"a.example.com,b.example.com;c.example.com", []string{"a.example.com", "b.example.com", "c.example.com"}},
		{"user@mail.example.com", []string{"mail.example.com"}},
		{"notexample.com my-example.com", nil},
		{"example.com.evil.test", nil},
		{"example.community example.com_x www.example.com-cdn.test", nil},
		{"%2Fwww.example.com", []string{"www.example.com"}},
		{"%252F%252Fdeep.example.com", []string{"deep.example.com"}},
		{`\x2fx.example.com \/s.example.com`, []string{"s.example.com", "x.example.com"}},
		{`\u002f\u002Fwide.example.com`, []string{"wide.example.com"}},
		{`"\nline.example.com"`, []string{"line.example.com"}},
		{"&#47;&#x2F;ent.example.com&quot;", []string{"ent.example.com"}},
		{"&#x2Fnosemi.example.com mx&#46;example&#46;com", []string{"mx.example.com", "nosemi.example.com"}},
		{"a..example.com trailing-.example.com", []string{"example.com"}},
		{"-dash.example.com *.wild.example.com", []string{"dash.example.com", "wild.example.com"}},
		{"\u50cf\u662fperf.example.com\u8fd9\u79cd", []string{"perf.example.com"}},
		{"\u212a.example.com", []string{"example.com"}}, // знак кельвина - не буква k
		{strings.Repeat("a", 63) + ".example.com", []string{strings.Repeat("a", 63) + ".example.com"}},
		{strings.Repeat("a", 64) + ".example.com", []string{"example.com"}},
		{strings.Repeat("a.", 122) + "example.com", nil},
	}
	for _, c := range cases {
		equal(t, c.text, PageNames("example.com", c.text, nil), c.want)
	}
}

func TestParseSPF(t *testing.T) {
	cases := []struct {
		txt                        string
		ok                         bool
		names, includes, ips, nets []string
	}{
		{
			txt: "v=spf1 mx a:mail-gw01.example.com a:mail-gw02.example.com/28 ip4:192.0.2.0/24 ip4:198.51.100.17" +
				" ip6:2001:db8:25::/48 include:_spf.example.com include:_spf.mailer.test ~all",
			ok:       true,
			names:    []string{"mail-gw01.example.com", "mail-gw02.example.com"},
			includes: []string{"_spf.example.com", "_spf.mailer.test"},
			ips:      []string{"198.51.100.17"},
			nets:     []string{"192.0.2.0/24"},
		},
		{txt: "v=spf1 redirect=_spf2.example.com", ok: true, includes: []string{"_spf2.example.com"}},
		{
			txt: "v=spf1 exists:%{i}._spf.example.com ptr:relay.example.com mx:mx-out.example.com/24//64 a/24" +
				" exp=explain._spf.%{d} exists:%{l}box.example.net -all",
			ok:    true,
			names: []string{"_spf.example.com", "relay.example.com", "mx-out.example.com", "example.net"},
		},
		{
			txt:      "V=SPF1 +IP4:203.0.113.64/27 ?include:spf.protection.example.com. -ALL",
			ok:       true,
			includes: []string{"spf.protection.example.com"},
			nets:     []string{"203.0.113.0/24"},
		},
		{
			txt: "v=spf1 ip4:198.18.4.0/23 ip4:198.18.5.128/25 ip4:198.18.8.0/22 ip4:198.18.0.0/21 ip4:203.0.113.7/32" +
				" ip6:2001:db8::/32 ip6:2001:db8::25 ip4:2001:db8::1 ip6:192.0.2.1 -all",
			ok:  true,
			ips: []string{"203.0.113.7", "2001:db8::25"},
			nets: []string{"198.18.4.0/24", "198.18.5.0/24", "198.18.8.0/24", "198.18.9.0/24", "198.18.10.0/24",
				"198.18.11.0/24"},
		},
		{txt: "v=spf1 ip4:192.0.2.999 ip4:203.0.113.5/40 include: junk:value a:192.0.2.1 a:localhost -all", ok: true},
		{txt: "v=DMARC1; p=reject; rua=mailto:dmarc@reports.example.com"},
		{txt: "v=spf10 a:mail.example.com"},
		{txt: ""},
	}
	for _, c := range cases {
		spf, ok := ParseSPF(c.txt)
		if ok != c.ok {
			t.Errorf("%q: ok=%v", c.txt, ok)
		}
		var nets []string
		for _, n := range spf.Nets {
			nets = append(nets, n.String())
		}
		equal(t, c.txt+" - имена", spf.Names, c.names)
		equal(t, c.txt+" - include", spf.Includes, c.includes)
		equal(t, c.txt+" - адреса", spf.IPs, c.ips)
		equal(t, c.txt+" - сети", nets, c.nets)
	}
}

func TestMailHosts(t *testing.T) {
	cases := []struct {
		txt  string
		want []string
	}{
		{
			"v=DMARC1; p=reject; rua=mailto:dmarc@reports.example.com!10m,mailto:agg@dmarc.vendor.test;" +
				" ruf=mailto:forensic@Example.COM",
			[]string{"dmarc.vendor.test", "example.com", "reports.example.com"},
		},
		{
			"v=TLSRPTv1; rua=https://tls.example.net/v1/report,mailto:tlsrpt@mta.example.com",
			[]string{"mta.example.com", "tls.example.net"},
		},
		{"v=TLSRPTv1; rua=https://user@in.example.com:8443/tls", []string{"in.example.com"}},
		{"v=DMARC1; p=none; rua=mailto:postmaster", nil},
		{"google-site-verification=abc123", nil},
	}
	for _, c := range cases {
		equal(t, c.txt, MailHosts(c.txt), c.want)
	}
}

func TestSOAMailHost(t *testing.T) {
	cases := map[string]string{
		"hostmaster.example.com.":     "example.com",
		"admin.DNS.example.com":       "dns.example.com",
		`first\.last.ops.example.com`: "ops.example.com",
		"nodots":                      "",
		"root.localhost.":             "",
		".":                           "",
	}
	for rname, want := range cases {
		if got := SOAMailHost(rname); got != want {
			t.Errorf("%q: %q, нужно %q", rname, got, want)
		}
	}
}

func TestIodefHost(t *testing.T) {
	cases := map[string]string{
		"mailto:security@pki.example.com":       "pki.example.com",
		`"https://caa-report.example.com/hook"`: "caa-report.example.com",
		"http://Report.Example.NET:8080/caa":    "report.example.net",
		"https://192.0.2.7/caa":                 "",
		"ca.example.net":                        "",
		"contact mailto:security@example.com":   "",
	}
	for value, want := range cases {
		if got := IodefHost(value); got != want {
			t.Errorf("%q: %q, нужно %q", value, got, want)
		}
	}
}

func TestMutate(t *testing.T) {
	long := strings.Repeat("a", 60)
	cases := []struct {
		name   string
		count  int
		has    []string
		hasNot []string
	}{
		{
			name:  "lk.example.com",
			count: 28,
			has: strings.Fields(`lk1.example.com lk5.example.com lk01.example.com lk05.example.com lk-dev.example.com
				beta-lk.example.com staging-lk.example.com lk-old.example.com lk.test.example.com dev.lk.example.com`),
			hasNot: strings.Fields(`lk.example.com lk0.example.com lk6.example.com lk-1.example.com lktest.example.com
				lk-prod.example.com lk.stage.example.com`),
		},
		{
			name:  "node-07.dc.example.com",
			count: 30 + 14 + 4,
			has: strings.Fields(`node-00.dc.example.com node-30.dc.example.com node-07-test.dc.example.com
				old-node-07.dc.example.com node-07.test.dc.example.com dev.node-07.dc.example.com`),
			hasNot: strings.Fields(`node-07.dc.example.com node-31.dc.example.com node-071.dc.example.com
				node-07.dc1.example.com node-07.dc.test.example.com`),
		},
		{
			name:   "sign2021.example.com",
			count:  40 + 14 + 4,
			has:    []string{"sign2001.example.com", "sign2041.example.com"},
			hasNot: strings.Fields("sign2000.example.com sign2042.example.com sign0.example.com sign20211.example.com"),
		},
		{
			name:   "host0007.example.com",
			count:  27 + 14 + 4,
			has:    []string{"host0000.example.com", "host0027.example.com"},
			hasNot: []string{"host0028.example.com", "host7.example.com"},
		},
		{
			name:   "r2d2.example.com",
			count:  30 + 30 + 14 + 4,
			has:    []string{"r0d2.example.com", "r30d2.example.com", "r2d0.example.com", "r2d30.example.com"},
			hasNot: []string{"r0d0.example.com"},
		},
		{name: "documentation-mirrors.example.com", count: 14 + 4, hasNot: []string{"documentation-mirrors1.example.com"}},
		{name: long + ".example.com", count: 4, has: []string{"test." + long + ".example.com"}},
		{name: "example.com"},
		{name: "www.example.net"},
		{name: "wwwexample.com"},
		{name: "_dmarc.example.com"},
	}
	for _, c := range cases {
		got := Mutate(c.name, "example.com")
		if len(got) != c.count {
			t.Errorf("%s: кандидатов %d, нужно %d: %v", c.name, len(got), c.count, got)
		}
		if len(sorted(got)) != len(got) {
			t.Errorf("%s: есть повторы: %v", c.name, got)
		}
		for _, name := range c.has {
			if !slices.Contains(got, name) {
				t.Errorf("%s: нет %s", c.name, name)
			}
		}
		for _, name := range c.hasNot {
			if slices.Contains(got, name) {
				t.Errorf("%s: лишнее %s", c.name, name)
			}
		}
	}
}

func TestReverse(t *testing.T) {
	net := netip.MustParsePrefix("192.0.2.0/24")
	if zone := ReverseZone(net); zone != "2.0.192.in-addr.arpa" {
		t.Errorf("зона: %q", zone)
	}
	names := ReverseNames(net)
	if len(names) != 256 || names[0] != "0.2.0.192.in-addr.arpa" || names[255] != "255.2.0.192.in-addr.arpa" {
		t.Errorf("имена: %d, %v", len(names), names)
	}
	for _, other := range []string{"192.0.2.0/25", "198.18.4.0/23", "2001:db8::/32"} {
		net := netip.MustParsePrefix(other)
		if zone, names := ReverseZone(net), ReverseNames(net); zone != "" || names != nil {
			t.Errorf("%s - не сеть /24: %q, имён %d", other, zone, len(names))
		}
	}
}
