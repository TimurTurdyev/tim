// Package reputation: кто из атакующих - массовый шум, а кто бьёт прицельно. Бесплатные списки без ключа
// (FireHOL level1, Spamhaus DROP, выходы Tor) скачиваются целиком раз в сутки; GreyNoise community
// спрашивается только про немногих прицельных, ответ хранится неделю.
package reputation

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
)

// sources: списки и как их назвать человеку.
var sources = []struct{ name, url string }{
	{"FireHOL", "https://iplists.firehol.org/files/firehol_level1.netset"},
	{"Spamhaus DROP", "https://www.spamhaus.org/drop/drop.txt"},
	{"Tor", "https://check.torproject.org/torbulkexitlist"},
}

var client = &http.Client{Timeout: 15 * time.Second}

// Lists: сети из списков; список, который не скачался, просто пропускается.
type Lists struct {
	nets map[string][]netip.Prefix
}

// Noise: что GreyNoise знает об адресе.
type Noise struct {
	Noise   bool   `json:"noise"` // массово сканирует интернет
	RIOT    bool   `json:"riot"`  // известный сервис: поисковики, CDN
	Name    string `json:"name,omitempty"`
	Message string `json:"message,omitempty"`
}

// Load: списки из кэша, старше суток - заново.
func Load() Lists {
	l := Lists{nets: map[string][]netip.Prefix{}}
	for _, s := range sources {
		lines, err := cache.Get(cache.Path(paths.State("reputation"), s.name), 24*time.Hour, func() ([]string, error) { return fetch(s.url) })
		if err != nil {
			continue
		}
		for _, line := range lines {
			if p, err := parsePrefix(line); err == nil {
				l.nets[s.name] = append(l.nets[s.name], p)
			}
		}
	}
	return l
}

func fetch(url string) ([]string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	var out []string
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 8<<20))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), ";")
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out, sc.Err()
}

func parsePrefix(s string) (netip.Prefix, error) {
	if strings.Contains(s, "/") {
		return netip.ParsePrefix(s)
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(a, a.BitLen()), nil
}

// In: в каких списках адрес. Частные и служебные адреса не в счёт: FireHOL держит их ради файрволов.
func (l Lists) In(ip string) []string {
	a, err := netip.ParseAddr(ip)
	if err != nil || a.IsPrivate() || a.IsLoopback() || a.IsUnspecified() || a.IsLinkLocalUnicast() {
		return nil
	}
	var out []string
	for _, s := range sources {
		for _, p := range l.nets[s.name] {
			if p.Contains(a) {
				out = append(out, s.name)
				break
			}
		}
	}
	return out
}

// Empty: ни один список не скачался - про шум сказать нечего.
func (l Lists) Empty() bool { return len(l.nets) == 0 }

// GreyNoise: ответ community API про адрес; без ключа, с ограничением частоты - поэтому кэш на неделю.
func GreyNoise(ip string) (Noise, bool) {
	if net.ParseIP(ip) == nil {
		return Noise{}, false
	}
	n, err := cache.Get(cache.Path(paths.State("greynoise"), ip), 7*24*time.Hour, func() (Noise, error) {
		resp, err := client.Get("https://api.greynoise.io/v3/community/" + ip)
		if err != nil {
			return Noise{}, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
			return Noise{}, fmt.Errorf("greynoise: %s", resp.Status)
		}
		var n Noise
		err = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&n)
		return n, err
	})
	return n, err == nil
}
