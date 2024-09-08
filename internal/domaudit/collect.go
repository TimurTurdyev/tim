package domaudit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
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
}

// Addr: адрес, его сеть и имена, которые на него смотрят.
type Addr struct {
	IP     string   `json:"ip"`
	PTR    string   `json:"ptr,omitempty"`
	AS     ASInfo   `json:"as,omitempty"`
	Hosts  []string `json:"hosts"`
	CDN    bool     `json:"cdn,omitempty"`
	Shared []string `json:"shared,omitempty"`
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

func (r *Report) addErr(mu *sync.Mutex, what string, err error) {
	mu.Lock()
	r.Errors = append(r.Errors, what+": "+err.Error())
	mu.Unlock()
}

// Collect собирает отчёт по домену из публичных источников.
func Collect(domain string, opt Options) *Report {
	rep := &Report{Domain: domain}
	var mu sync.Mutex
	names := map[string]string{domain: "апекс"}

	var wg sync.WaitGroup
	wg.Go(func() {
		if opt.SkipCRT {
			return
		}
		if opt.Notify != nil {
			if c, ok := readCache(domain); ok && !opt.FreshCRT && time.Since(c.Fetched) < CacheTTL {
				opt.Notify("логи сертификатов из кэша, обновить: --fresh")
			} else {
				opt.Notify("спрашиваю логи сертификатов")
			}
		}
		subs, related, cached, err := fromCRT(domain, opt.FreshCRT, !opt.SkipNeighbors)
		mu.Lock()
		defer mu.Unlock()
		if err != nil && subs == nil {
			rep.Errors = append(rep.Errors, "crt.sh: "+err.Error())
			return
		}
		if err != nil {
			rep.Errors = append(rep.Errors, "crt.sh: "+err.Error())
		}
		rep.CRTCached = cached
		for _, s := range subs {
			if names[s] == "" {
				names[s] = "сертификат"
			}
		}
		rep.Related = related
	})
	wg.Go(func() {
		for _, n := range bruteforce(domain) {
			mu.Lock()
			if names[n] == "" {
				names[n] = "перебор"
			}
			mu.Unlock()
		}
	})
	wg.Wait()

	// Резолв всех имён, по 16 одновременно.
	type resolved struct {
		name string
		ips  []string
	}
	found := make([]resolved, 0, len(names))
	sem := make(chan struct{}, 16)
	var rwg sync.WaitGroup
	var rmu sync.Mutex
	for n := range names {
		rwg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			ips := lookup(n)
			if len(ips) == 0 {
				return
			}
			rmu.Lock()
			found = append(found, resolved{n, ips})
			rmu.Unlock()
		})
	}
	rwg.Wait()
	sort.Slice(found, func(i, j int) bool { return found[i].name < found[j].name })

	byIP := map[string][]string{}
	for _, f := range found {
		rep.Hosts = append(rep.Hosts, Host{Name: f.name, IPs: f.ips, Source: names[f.name]})
		for _, ip := range f.ips {
			byIP[ip] = append(byIP[ip], f.name)
		}
	}

	ips := make([]string, 0, len(byIP))
	for ip := range byIP {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	asn := cymru(ips)
	for _, ip := range ips {
		a := Addr{IP: ip, AS: asn[ip], Hosts: byIP[ip]}
		a.CDN = IsCDN(a.AS.Name)
		rep.Addrs = append(rep.Addrs, a)
	}

	var pwg sync.WaitGroup
	psem := make(chan struct{}, 16)
	for i := range rep.Addrs {
		pwg.Go(func() {
			psem <- struct{}{}
			defer func() { <-psem }()
			rep.Addrs[i].PTR = ptr(rep.Addrs[i].IP)
		})
	}
	for i := range rep.Hosts {
		pwg.Go(func() {
			psem <- struct{}{}
			defer func() { <-psem }()
			probe(&rep.Hosts[i])
		})
	}
	pwg.Wait()

	// Счётчики берём с главной страницы домена.
	for _, h := range rep.Hosts {
		if (h.Name == domain || h.Name == "www."+domain) && h.Status > 0 {
			if tr := trackersOf(h.Scheme + "://" + h.Name); len(tr) > 0 {
				rep.Trackers = tr
				break
			}
		}
	}

	if !opt.SkipNeighbors {
		for i := range rep.Addrs {
			a := &rep.Addrs[i]
			if a.CDN || !contains(a.Hosts, domain) {
				continue
			}
			list, err := neighbors(a.IP, domain)
			if err != nil {
				rep.addErr(&mu, "соседи по IP", err)
				break
			}
			a.Shared = list
			break
		}
	}
	return rep
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// certSources: откуда берутся имена. CertSpotter быстрый, crt.sh глубже по истории, hostsearch видит то,
// чего нет в сертификатах. Опрашиваются параллельно, результат объединяется.
func certSources(domain string, withQuota bool) (subs, related []string, used []string, err error) {
	type res struct {
		name    string
		subs    []string
		related []string
		err     error
	}
	jobs := []struct {
		name string
		fn   func(string) ([]string, []string, error)
	}{
		{"certspotter", fromCertSpotter},
		{"crt.sh", crtOnce},
	}
	if withQuota {
		jobs = append(jobs, struct {
			name string
			fn   func(string) ([]string, []string, error)
		}{"hostsearch", fromHostSearch})
	}
	out := make([]res, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Go(func() {
			s, r, e := j.fn(domain)
			out[i] = res{name: j.name, subs: s, related: r, err: e}
		})
	}
	wg.Wait()
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
	if has && !fresh && time.Since(c.Fetched) < CacheTTL {
		return c.Subs, c.Related, c.Fetched, nil
	}
	subs, related, _, err = certSources(domain, withQuota)
	if err == nil {
		writeCache(domain, subs, related)
		return subs, related, time.Time{}, nil
	}
	if has {
		return c.Subs, c.Related, c.Fetched, errCrtStale
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

func bruteforce(domain string) []string {
	var out []string
	var mu sync.Mutex
	sem := make(chan struct{}, 24)
	var wg sync.WaitGroup
	for _, p := range Common {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			n := p + "." + domain
			if len(lookup(n)) > 0 {
				mu.Lock()
				out = append(out, n)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
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

// cymru спрашивает сведения обо всех адресах одним соединением.
func cymru(ips []string) map[string]ASInfo {
	if len(ips) == 0 {
		return nil
	}
	conn, err := net.DialTimeout("tcp", "whois.cymru.com:43", httpTimeout)
	if err != nil {
		return nil
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	q := "begin\nverbose\n" + strings.Join(ips, "\n") + "\nend\n"
	if _, err := io.WriteString(conn, q); err != nil {
		return nil
	}
	data, _ := io.ReadAll(io.LimitReader(conn, 1<<20))
	return ParseCymru(string(data))
}

// probe: отвечает ли имя по HTTPS, иначе по HTTP.
func probe(h *Host) {
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		_ = resp.Body.Close()
		h.Scheme, h.Status, h.Server = scheme, resp.StatusCode, resp.Header.Get("Server")
		h.Title = Title(string(body))
		// Редирект отмечаем, только если ушли на другой адрес, а не просто на "/" того же имени.
		u := resp.Request.URL
		if u.Host != h.Name || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
			h.Final = u.String()
		}
		return
	}
}

func trackersOf(target string) []Tracker {
	client := &http.Client{Timeout: httpTimeout}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) tim")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512<<10))
	return Trackers(string(body))
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
