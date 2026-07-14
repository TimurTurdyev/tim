package domaudit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/par"
)

var errCrtStale = errors.New("не ответил, показан прошлый список имён")

var errCrtBusy = errors.New("ответил не JSON")

var errNoQuota = errors.New("дневной лимит бесплатного сервиса исчерпан")

const (
	dnsTimeout  = 3 * time.Second
	httpTimeout = 5 * time.Second
	crtTimeout  = 45 * time.Second
)

// Host: найденное имя и что о нём известно.
type Host struct {
	Name   string   `json:"name"`
	IPs    []string `json:"ips,omitempty"`
	Status int      `json:"status,omitempty"`
	Scheme string   `json:"scheme,omitempty"`
	Server string   `json:"server,omitempty"`
	Title  string   `json:"title,omitempty"`
	Final  string   `json:"final,omitempty"`
	Source string   `json:"source"`

	trackers []Tracker // только у главной страницы
}

// Addr: адрес, его сеть и имена, которые на него смотрят.
type Addr struct {
	IP     string     `json:"ip"`
	PTR    string     `json:"ptr,omitempty"`
	AS     asn.ASInfo `json:"as,omitempty"`
	Hosts  []string   `json:"hosts"`
	CDN    bool       `json:"cdn,omitempty"`
	Shared []string   `json:"shared,omitempty"`
}

// Report: всё, что собрано по домену.
type Report struct {
	Domain   string    `json:"domain"`
	Hosts    []Host    `json:"hosts"`
	Addrs    []Addr    `json:"addrs"`
	Related  []string  `json:"related,omitempty"`
	Trackers []Tracker `json:"trackers,omitempty"`
	Errors   []string  `json:"errors,omitempty"`
	// CRTCached: не пусто, если список из логов сертификатов взят из кэша.
	CRTCached time.Time `json:"crt_cached,omitzero"`
}

// Options: что собирать.
type Options struct {
	SkipCRT       bool
	FreshCRT      bool
	SkipNeighbors bool
	Notify        func(string)
}

// Collect собирает отчёт по домену из публичных источников: имена, их адреса и сети, ответы
// сайтов, счётчики главной страницы и соседей по адресу.
func Collect(domain string, opt Options) *Report {
	rep := &Report{Domain: domain}
	names, known := rep.discover(opt)
	rep.resolve(names, known)
	rep.probe()
	rep.Trackers = homeTrackers(domain, rep.Hosts)
	if !opt.SkipNeighbors {
		rep.findNeighbors()
	}
	return rep
}

// discover: имена домена из логов сертификатов и перебора - откуда каждое взялось, и адреса тех,
// что уже резолвились при переборе.
func (r *Report) discover(opt Options) (names map[string]string, known map[string][]string) {
	var mu sync.Mutex
	names = map[string]string{r.Domain: "апекс"}
	addNames := func(list []string, source string) {
		mu.Lock()
		defer mu.Unlock()
		for _, n := range list {
			if names[n] == "" {
				names[n] = source
			}
		}
	}
	var wg sync.WaitGroup
	wg.Go(func() {
		if opt.SkipCRT {
			return
		}
		if opt.Notify != nil {
			if c, ok := readCache(r.Domain); ok && !opt.FreshCRT && c.Fresh(CacheTTL) {
				opt.Notify("логи сертификатов из кэша, обновить: --fresh")
			} else {
				opt.Notify("спрашиваю логи сертификатов")
			}
		}
		subs, related, cached, err := fromCRT(r.Domain, opt.FreshCRT, !opt.SkipNeighbors)
		mu.Lock()
		if err != nil {
			r.Errors = append(r.Errors, "crt.sh: "+err.Error())
		}
		if err == nil || subs != nil {
			r.CRTCached, r.Related = cached, related
		}
		mu.Unlock()
		addNames(subs, "сертификат")
	})
	wg.Go(func() {
		known = bruteforce(r.Domain)
		addNames(slices.Sorted(maps.Keys(known)), "перебор")
	})
	wg.Wait()
	return names, known
}

// resolve: адреса имён, по 16 одновременно, и чьи это сети. Найденные перебором уже резолвились.
func (r *Report) resolve(names map[string]string, known map[string][]string) {
	all := slices.Sorted(maps.Keys(names))
	byIP := map[string][]string{}
	resolved := par.Map(all, 16, func(name string) []string {
		if ips, ok := known[name]; ok {
			return ips
		}
		return lookup(name)
	})
	for i, ips := range resolved {
		if len(ips) == 0 {
			continue
		}
		r.Hosts = append(r.Hosts, Host{Name: all[i], IPs: ips, Source: names[all[i]]})
		for _, ip := range ips {
			byIP[ip] = append(byIP[ip], all[i])
		}
	}
	ips := slices.Sorted(maps.Keys(byIP))
	owners := asn.Cymru(ips)
	for _, ip := range ips {
		a := Addr{IP: ip, AS: owners[ip], Hosts: byIP[ip]}
		a.CDN = asn.IsCDN(a.AS.Name)
		r.Addrs = append(r.Addrs, a)
	}
}

// probe: обратные имена адресов и ответы сайтов, по 16 одновременно. Главная страница домена
// читается тут же и целиком: счётчики берутся из неё, второй раз её не качаем.
func (r *Report) probe() {
	probes := par.NewGroup(16)
	for i := range r.Addrs {
		probes.Go(func() { r.Addrs[i].PTR = ptr(r.Addrs[i].IP) })
	}
	for i := range r.Hosts {
		home := r.Hosts[i].Name == r.Domain || r.Hosts[i].Name == "www."+r.Domain
		probes.Go(func() { probe(&r.Hosts[i], home) })
	}
	probes.Wait()
}

// homeTrackers: счётчики с главной страницы домена.
func homeTrackers(domain string, hosts []Host) []Tracker {
	for _, h := range hosts {
		if (h.Name == domain || h.Name == "www."+domain) && len(h.trackers) > 0 {
			return h.trackers
		}
	}
	return nil
}

// findNeighbors: чужие сайты на первом своём, не CDN-адресе апекса.
func (r *Report) findNeighbors() {
	for i := range r.Addrs {
		a := &r.Addrs[i]
		if a.CDN || !slices.Contains(a.Hosts, r.Domain) {
			continue
		}
		list, err := neighbors(a.IP, r.Domain)
		if err != nil {
			r.Errors = append(r.Errors, "соседи по IP: "+err.Error())
			return
		}
		a.Shared = list
		return
	}
}

// certSources: откуда берутся имена. CertSpotter быстрый, crt.sh глубже по истории, hostsearch видит то,
// чего нет в сертификатах. Опрашиваются параллельно, результат объединяется.
func certSources(domain string, withQuota bool) (subs, related []string, used []string, err error) {
	type source struct {
		name string
		fn   func(string) ([]string, []string, error)
	}
	type res struct {
		name    string
		subs    []string
		related []string
		err     error
	}
	sources := []source{{"certspotter", fromCertSpotter}, {"crt.sh", crtOnce}}
	if withQuota {
		sources = append(sources, source{"hostsearch", fromHostSearch})
	}
	out := par.Map(sources, len(sources), func(src source) res {
		s, r, e := src.fn(domain)
		return res{name: src.name, subs: s, related: r, err: e}
	})
	seenS, seenR := map[string]bool{}, map[string]bool{}
	var fails []string
	for _, r := range out {
		if r.err != nil {
			fails = append(fails, r.name+" ("+r.err.Error()+")")
			continue
		}
		used = append(used, r.name)
		for _, n := range r.subs {
			if !seenS[n] {
				seenS[n] = true
				subs = append(subs, n)
			}
		}
		for _, n := range r.related {
			if !seenR[n] {
				seenR[n] = true
				related = append(related, n)
			}
		}
	}
	sort.Strings(subs)
	sort.Strings(related)
	if len(used) == 0 {
		return nil, nil, nil, errors.New("источники не ответили: " + strings.Join(fails, ", "))
	}
	return subs, related, used, nil
}

// fromCRT: имена из источников с кэшем на сутки; при полном отказе берётся прошлый список.
func fromCRT(domain string, fresh, withQuota bool) (subs, related []string, cached time.Time, err error) {
	c, has := readCache(domain)
	if has && !fresh && c.Fresh(CacheTTL) {
		return c.Value.Subs, c.Value.Related, c.Fetched, nil
	}
	subs, related, _, err = certSources(domain, withQuota)
	if err == nil {
		writeCache(domain, subs, related)
		return subs, related, time.Time{}, nil
	}
	if has {
		return c.Value.Subs, c.Value.Related, c.Fetched, errCrtStale
	}
	return nil, nil, time.Time{}, err
}

func getJSON(target string, timeout time.Duration) ([]byte, error) {
	c := &http.Client{Timeout: timeout}
	resp, err := c.Get(target)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	return data, nil
}

func fromCertSpotter(domain string) ([]string, []string, error) {
	data, err := getJSON("https://api.certspotter.com/v1/issuances?domain="+url.QueryEscape(domain)+
		"&include_subdomains=true&expand=dns_names", 20*time.Second)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		return nil, nil, errCrtBusy
	}
	return ParseCertSpotter(data, domain)
}

func fromHostSearch(domain string) ([]string, []string, error) {
	data, err := getJSON("https://api.hackertarget.com/hostsearch/?q="+url.QueryEscape(domain), 20*time.Second)
	if err != nil {
		return nil, nil, err
	}
	subs, err := ParseHostSearch(string(data), domain)
	return subs, nil, err
}

func crtOnce(domain string) ([]string, []string, error) {
	data, err := getJSON("https://crt.sh/?q="+url.QueryEscape("%."+domain)+"&output=json", crtTimeout)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("[")) {
		return nil, nil, errCrtBusy
	}
	return ParseCRT(data, domain)
}

// bruteforce: частые имена, которые резолвятся, с их адресами. При wildcard-записи резолвится любое
// имя: такие ответы совпадают с ответом на заведомо несуществующее имя и отбрасываются.
func bruteforce(domain string) map[string][]string {
	wildcard := strings.Join(lookup("tim-nx-"+strconv.FormatInt(time.Now().UnixNano(), 36)+"."+domain), " ")
	out := map[string][]string{}
	for i, ips := range par.Map(Common, 24, func(p string) []string { return lookup(p + "." + domain) }) {
		if len(ips) > 0 && (wildcard == "" || strings.Join(ips, " ") != wildcard) {
			out[Common[i]+"."+domain] = ips
		}
	}
	return out
}

func lookup(name string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, name)
	if err != nil {
		return nil
	}
	var out []string
	for _, a := range addrs {
		out = append(out, a.IP.String())
	}
	sort.Strings(out)
	return out
}

func ptr(ip string) string {
	ctx, cancel := context.WithTimeout(context.Background(), dnsTimeout)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(ctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

// probe: отвечает ли имя по HTTPS, иначе по HTTP; у главной страницы - ещё и счётчики.
func probe(h *Host, home bool) {
	client := &http.Client{
		Timeout:   httpTimeout,
		Transport: &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: httpTimeout},
	}
	for _, scheme := range []string{"https", "http"} {
		req, err := http.NewRequest(http.MethodGet, scheme+"://"+h.Name, nil)
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) tim")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		limit := int64(256 << 10)
		if home {
			limit = 512 << 10
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, limit))
		_ = resp.Body.Close()
		h.Scheme, h.Status, h.Server = scheme, resp.StatusCode, resp.Header.Get("Server")
		h.Title = Title(string(body))
		if home {
			h.trackers = Trackers(string(body))
		}
		// Редирект отмечаем, только если ушли на другой адрес, а не просто на "/" того же имени.
		u := resp.Request.URL
		if u.Host != h.Name || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
			h.Final = u.String()
		}
		return
	}
}

func neighbors(ip, self string) ([]string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get("https://api.hackertarget.com/reverseiplookup/?q=" + url.QueryEscape(ip))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	out := string(data)
	if strings.Contains(out, "API count exceeded") {
		return nil, errNoQuota
	}
	return ParseNeighbors(out, self), nil
}
