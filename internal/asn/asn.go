// Package asn: сети автономной системы - чтобы увести в обход VPN владельца сайта целиком,
// а не адреса одного домена, у которого поддомены и CDN живут в других местах.
package asn

import (
	"cmp"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/par"
)

// TTL: анонсы меняются, но не каждый час; сутки - разумный срок.
const TTL = 24 * time.Hour

// retryAfter: пауза после отказа RIPEstat, maxBody: больше ответ не бывает, а память не резиновая.
const (
	retryAfter = 10 * time.Minute
	maxBody    = 4 << 20
)

var client = &http.Client{Timeout: 20 * time.Second}

var (
	failMu   sync.Mutex
	failedAt = map[string]time.Time{}
)

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
// Одна сортировка и стек: вложенная сеть идёт сразу за своей, а склейка проверяется только с вершиной.
func Aggregate(list []string) []string {
	var ps []netip.Prefix
	for _, s := range list {
		if p, err := netip.ParsePrefix(s); err == nil && p.Addr().Is4() {
			ps = append(ps, p.Masked())
		}
	}
	slices.SortFunc(ps, func(a, b netip.Prefix) int {
		return cmp.Or(a.Addr().Compare(b.Addr()), cmp.Compare(a.Bits(), b.Bits()))
	})
	var stack []netip.Prefix
	for _, p := range ps {
		if n := len(stack); n > 0 && stack[n-1].Bits() <= p.Bits() && stack[n-1].Contains(p.Addr()) {
			continue
		}
		stack = append(stack, p)
		for n := len(stack); n >= 2; n = len(stack) {
			if merged, ok := siblings(stack[n-2], stack[n-1]); ok {
				stack = append(stack[:n-2], merged)
				continue
			}
			break
		}
	}
	out := make([]string, len(stack))
	for i, p := range stack {
		out[i] = p.String()
	}
	return out
}

// siblings: две половины одной сети - тогда она сама.
func siblings(a, b netip.Prefix) (netip.Prefix, bool) {
	if a.Bits() != b.Bits() || a.Bits() == 0 {
		return netip.Prefix{}, false
	}
	parent, _ := a.Addr().Prefix(a.Bits() - 1)
	return parent, parent.Addr() == a.Addr() && parent.Contains(b.Addr())
}

// Prefixes: склеенные IPv4-сети AS из кэша в dir или из RIPEstat. Ошибка сети при живом кэше - не ошибка:
// старый список полезнее пустого, иначе обход пропадёт из-за недоступности статистики.
func Prefixes(dir, as string) ([]string, error) {
	as = strings.TrimPrefix(strings.ToUpper(as), "AS")
	return cache.Get(cache.Path(filepath.Join(dir, "asn"), as), TTL, func() ([]string, error) {
		if wait := retryIn(as); wait > 0 {
			return nil, errors.New("RIPEstat не отвечал, повтор через " + wait.Round(time.Minute).String())
		}
		list, err := fetch(as)
		if err != nil {
			failed(as)
			return nil, err
		}
		if list = Aggregate(list); len(list) == 0 {
			return nil, errors.New("у AS" + as + " нет анонсированных IPv4-сетей")
		}
		return list, nil
	})
}

// retryIn: сколько ждать до новой попытки после отказа RIPEstat. Страж зовёт Prefixes каждый
// цикл, и без паузы каждый цикл висел бы на таймауте, пока источник лежит.
func retryIn(as string) time.Duration {
	failMu.Lock()
	defer failMu.Unlock()
	return time.Until(failedAt[as].Add(retryAfter))
}

func failed(as string) {
	failMu.Lock()
	failedAt[as] = time.Now()
	failMu.Unlock()
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
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBody {
		return nil, errors.New("ответ RIPEstat больше 4 MB")
	}
	return data, nil
}

// Origins: все AS, из которых виден каждый из адресов, без повторов; адреса спрашиваются параллельно.
func Origins(ips []string) ([]string, error) {
	type answer struct {
		asns []string
		err  error
	}
	answers := par.Map(ips, 8, func(ip string) answer {
		data, err := getJSON("https://stat.ripe.net/data/network-info/data.json?resource=" + ip)
		if err != nil {
			return answer{err: err}
		}
		list, err := ParseNetworkInfo(data)
		return answer{list, err}
	})
	seen := map[string]bool{}
	var out []string
	var last error
	for _, a := range answers {
		if a.err != nil {
			last = a.err
		}
		for _, as := range a.asns {
			if !seen[as] {
				seen[as] = true
				out = append(out, as)
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
