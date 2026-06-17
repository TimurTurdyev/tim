// Package asn: сети автономной системы - чтобы увести в обход VPN владельца сайта целиком,
// а не адреса одного домена, у которого поддомены и CDN живут в других местах.
package asn

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// TTL: анонсы меняются, но не каждый час; сутки - разумный срок.
const TTL = 24 * time.Hour

var client = &http.Client{Timeout: 20 * time.Second}

// ParseRIPEstat разбирает ответ announced-prefixes и оставляет только IPv4.
func ParseRIPEstat(data []byte) ([]string, error) {
	var doc struct {
		Status string `json:"status"`
		Data   struct {
			Prefixes []struct {
				Prefix string `json:"prefix"`
			} `json:"prefixes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if doc.Status != "" && doc.Status != "ok" {
		return nil, errors.New("RIPEstat: " + doc.Status)
	}
	var out []string
	for _, p := range doc.Data.Prefixes {
		if pr, err := netip.ParsePrefix(p.Prefix); err == nil && pr.Addr().Is4() {
			out = append(out, pr.Masked().String())
		}
	}
	return out, nil
}

// Aggregate убирает вложенные сети и склеивает соседние половины в одну: меньше маршрутов в таблице.
func Aggregate(list []string) []string {
	var ps []netip.Prefix
	for _, s := range list {
		if p, err := netip.ParsePrefix(s); err == nil && p.Addr().Is4() {
			ps = append(ps, p.Masked())
		}
	}
	for changed := true; changed; {
		changed = false
		sort.Slice(ps, func(i, j int) bool {
			if c := ps[i].Addr().Compare(ps[j].Addr()); c != 0 {
				return c < 0
			}
			return ps[i].Bits() < ps[j].Bits()
		})
		var out []netip.Prefix
		for _, p := range ps {
			if n := len(out); n > 0 && out[n-1].Contains(p.Addr()) && out[n-1].Bits() <= p.Bits() {
				continue
			}
			if n := len(out); n > 0 && out[n-1].Bits() == p.Bits() && p.Bits() > 0 {
				parent, _ := out[n-1].Addr().Prefix(p.Bits() - 1)
				if parent.Contains(p.Addr()) && parent.Addr() == out[n-1].Addr() {
					out[n-1] = parent
					changed = true
					continue
				}
			}
			out = append(out, p)
		}
		ps = out
	}
	res := make([]string, len(ps))
	for i, p := range ps {
		res[i] = p.String()
	}
	return res
}

type cache struct {
	Fetched  time.Time `json:"fetched"`
	Prefixes []string  `json:"prefixes"`
}

// Prefixes: склеенные IPv4-сети AS из кэша в dir или из RIPEstat. Ошибка сети при живом кэше - не ошибка:
// старый список полезнее пустого, иначе обход пропадёт из-за недоступности статистики.
func Prefixes(dir, as string) ([]string, error) {
	as = strings.TrimPrefix(strings.ToUpper(as), "AS")
	path := filepath.Join(dir, "asn", as+".json")
	var old *cache
	if data, err := os.ReadFile(path); err == nil {
		var c cache
		if json.Unmarshal(data, &c) == nil && len(c.Prefixes) > 0 {
			if time.Since(c.Fetched) < TTL {
				return c.Prefixes, nil
			}
			old = &c
		}
	}
	list, err := fetch(as)
	if err != nil {
		if old != nil {
			return old.Prefixes, nil
		}
		return nil, err
	}
	list = Aggregate(list)
	if len(list) == 0 {
		if old != nil {
			return old.Prefixes, nil
		}
		return nil, errors.New("у AS" + as + " нет анонсированных IPv4-сетей")
	}
	if data, err := json.Marshal(cache{Fetched: time.Now(), Prefixes: list}); err == nil {
		_ = runner.WriteAtomic(path, data, 0o644)
	}
	return list, nil
}

func fetch(as string) ([]string, error) {
	data, err := getJSON("https://stat.ripe.net/data/announced-prefixes/data.json?resource=AS" + as)
	if err != nil {
		return nil, err
	}
	return ParseRIPEstat(data)
}

// ParseNetworkInfo разбирает network-info: все AS, из которых сейчас виден адрес. Их бывает несколько:
// один блок адресов владелец часто анонсирует сразу из основной и дочерней AS.
func ParseNetworkInfo(data []byte) ([]string, error) {
	var doc struct {
		Data struct {
			ASNs []string `json:"asns"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc.Data.ASNs, nil
}

// ParseHolder разбирает as-overview: кто держит AS.
func ParseHolder(data []byte) string {
	var doc struct {
		Data struct {
			Holder string `json:"holder"`
		} `json:"data"`
	}
	_ = json.Unmarshal(data, &doc)
	return doc.Data.Holder
}

func getJSON(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, errors.New("RIPEstat ответил " + resp.Status)
	}
	var b strings.Builder
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil || b.Len() > 4<<20 {
			break
		}
	}
	return []byte(b.String()), nil
}

// Origins: все AS, из которых виден каждый из адресов, без повторов.
func Origins(ips []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	var last error
	for _, ip := range ips {
		data, err := getJSON("https://stat.ripe.net/data/network-info/data.json?resource=" + ip)
		if err != nil {
			last = err
			continue
		}
		list, err := ParseNetworkInfo(data)
		if err != nil {
			last = err
			continue
		}
		for _, a := range list {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	if len(out) == 0 && last != nil {
		return nil, last
	}
	return out, nil
}

// Holder: название владельца AS, пусто если не узнали.
func Holder(as string) string {
	data, err := getJSON("https://stat.ripe.net/data/as-overview/data.json?resource=AS" + strings.TrimPrefix(strings.ToUpper(as), "AS"))
	if err != nil {
		return ""
	}
	return ParseHolder(data)
}
