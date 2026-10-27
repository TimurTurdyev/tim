// Package twins: двойники домена - опечатки, похожие буквы и та же метка в других зонах; какие из них
// зарегистрированы. Так ищут фишинг под свой сайт и почту (как dnstwist).
package twins

import (
	"context"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/par"
)

// keyboard: соседние клавиши qwerty - самые частые опечатки.
var (
	keyboard = map[rune]string{
		'q': "wa", 'w': "qeas", 'e': "wrds", 'r': "etdf", 't': "ryfg", 'y': "tugh", 'u': "yihj", 'i': "uojk", 'o': "ipkl",
		'p': "ol", 'a': "qwsz", 's': "awedxz", 'd': "serfcx", 'f': "drtgvc", 'g': "ftyhbv", 'h': "gyujnb", 'j': "huikmn",
		'k': "jiolm", 'l': "kop", 'z': "asx", 'x': "zsdc", 'c': "xdfv", 'v': "cfgb", 'b': "vghn", 'n': "bhjm", 'm': "njk",
	}
	glyphs = [][2]string{{"o", "0"}, {"l", "1"}, {"i", "1"}, {"i", "l"}, {"rn", "m"}, {"m", "rn"}, {"vv", "w"}, {"w", "vv"}, {"cl", "d"}}
	zones  = []string{"com", "net", "org", "ru", "su", "info", "biz", "io", "online", "site", "store", "shop", "pro", "xn--p1ai"}
)

// Twin: зарегистрированный двойник.
type Twin struct {
	Name string   `json:"name"`
	How  string   `json:"how"` // опечатка, похожие буквы, другая зона
	IPs  []string `json:"ips,omitempty"`
	MX   bool     `json:"mx"` // принимает почту - для фишинга по почте это главное
}

// Candidates: варианты домена example.com и как получен каждый; сам домен не входит.
func Candidates(domain string) map[string]string {
	label, zone, ok := strings.Cut(strings.ToLower(domain), ".")
	out := map[string]string{}
	if !ok || label == "" {
		return out
	}
	add := func(l, z, how string) {
		name := l + "." + z
		if valid(l) && name != domain && out[name] == "" {
			out[name] = how
		}
	}
	r := []rune(label)
	ascii := !slices.ContainsFunc(r, func(c rune) bool { return c > 127 })
	for i := range r {
		dropped := "опечатка"
		if r[i] == '-' {
			dropped = "дефис"
		}
		add(string(r[:i])+string(r[i+1:]), zone, dropped)
		add(string(r[:i+1])+string(r[i:]), zone, "опечатка")
		if i+1 < len(r) {
			s := slices.Clone(r)
			s[i], s[i+1] = s[i+1], s[i]
			add(string(s), zone, "опечатка")
			if r[i] != '-' && r[i+1] != '-' {
				add(string(r[:i+1])+"-"+string(r[i+1:]), zone, "дефис")
			}
		}
		for _, k := range keyboard[r[i]] {
			s := slices.Clone(r)
			s[i] = k
			add(string(s), zone, "опечатка")
		}
	}
	if ascii {
		for _, g := range glyphs {
			for i := 0; ; {
				j := strings.Index(label[i:], g[0])
				if j < 0 {
					break
				}
				add(label[:i+j]+g[1]+label[i+j+len(g[0]):], zone, "похожие буквы")
				i += j + 1
			}
		}
	}
	for _, z := range zones {
		add(label, z, "другая зона")
	}
	return out
}

func valid(l string) bool {
	return l != "" && len(l) <= 63 && !strings.HasPrefix(l, "-") && !strings.HasSuffix(l, "-") && !strings.Contains(l, "--")
}

// Registered: какие варианты зарегистрированы - есть NS; адреса и MX для строки. По 32 запроса разом.
func Registered(cands map[string]string) []Twin {
	names := make([]string, 0, len(cands))
	for n := range cands {
		names = append(names, n)
	}
	slices.Sort(names)
	found := par.Map(names, 32, func(name string) *Twin {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		if ns, err := net.DefaultResolver.LookupNS(ctx, name); err != nil || len(ns) == 0 {
			return nil
		}
		t := &Twin{Name: name, How: cands[name]}
		t.IPs, _ = net.DefaultResolver.LookupHost(ctx, name)
		mx, _ := net.DefaultResolver.LookupMX(ctx, name)
		t.MX = len(mx) > 0
		return t
	})
	var out []Twin
	for _, t := range found {
		if t != nil {
			out = append(out, *t)
		}
	}
	return out
}
