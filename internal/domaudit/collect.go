package domaudit

import (
	"bytes"
	"errors"
	"io"
	"maps"
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
	"github.com/TimurTurdyev/tim/internal/ui"
)

var errCrtStale = errors.New("не ответил, показан прошлый список имён")

var errCrtBusy = errors.New("ответил не JSON")

var errNoQuota = errors.New("дневной лимит бесплатного сервиса исчерпан")

const crtTimeout = 45 * time.Second

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
	// CNAME: куда ведёт имя; Takeover - его можно захватить: цель у чужого сервиса никем не занята.
	CNAME    string    `json:"cname,omitempty"`
	Takeover *Takeover `json:"takeover,omitempty"`
	// CertError: почему браузер не примет сертификат имени; CertName - на какое имя он выдан.
	CertError string `json:"cert_error,omitempty"`
	CertName  string `json:"cert_name,omitempty"`
	// Internal: все адреса имени внутренние - в интернете за ним никого нет.
	Internal bool `json:"internal,omitempty"`
	// Unchecked: имя за пределом сбора, по HTTP его не спрашивали.
	Unchecked bool `json:"unchecked,omitempty"`

	trackers  []Tracker // только у главной страницы
	certNames []string  // все имена сертификата
}

// Addr: адрес, его сеть и имена, которые на него смотрят. CDN - узел раздачи, настоящий сервер за ним не виден;
// Own - сервер самого домена: имя ведёт сюда без CNAME на чужой сервис; Internal - адрес не из интернета.
type Addr struct {
	IP       string     `json:"ip"`
	PTR      string     `json:"ptr,omitempty"`
	AS       asn.ASInfo `json:"as,omitempty"`
	Hosts    []string   `json:"hosts"`
	CDN      bool       `json:"cdn,omitempty"`
	Own      bool       `json:"own,omitempty"`
	Internal bool       `json:"internal,omitempty"`
	Shared   []string   `json:"shared,omitempty"`

	direct bool     // имя домена ведёт сюда само, а не через чужой сервис
	cnames []string // куда по дороге ведут имена
}

// Report: всё, что собрано по домену.
type Report struct {
	Domain   string    `json:"domain"`
	Hosts    []Host    `json:"hosts"`
	Addrs    []Addr    `json:"addrs"`
	Related  []string  `json:"related,omitempty"`
	Trackers []Tracker `json:"trackers,omitempty"`
	Errors   []string  `json:"errors,omitempty"`
	Notes    []string  `json:"notes,omitempty"` // что осталось за пределами сбора
	// CRTCached: не пусто, если список из логов сертификатов взят из кэша.
	CRTCached time.Time `json:"crt_cached,omitzero"`
}

// Options: что собирать. Quiet - без сторонних сервисов с именами, соседями, местом и портами: остаются DNS,
// регистрация, владелец сети и запросы к самому сайту.
type Options struct {
	Quiet         bool
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
	par.All(rep.probe, rep.classify)
	rep.Trackers = homeTrackers(domain, rep.Hosts)
	if !opt.Quiet && !opt.SkipNeighbors {
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
		if opt.Quiet {
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

// homeTrackers: счётчики с главной страницы домена.
func homeTrackers(domain string, hosts []Host) []Tracker {
	for _, h := range hosts {
		if (h.Name == domain || h.Name == "www."+domain) && len(h.trackers) > 0 {
			return h.trackers
		}
	}
	return nil
}

// findNeighbors: чужие сайты на первом своём адресе апекса.
func (r *Report) findNeighbors() {
	for i := range r.Addrs {
		a := &r.Addrs[i]
		if !a.Own || !slices.Contains(a.Hosts, r.Domain) {
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
		return nil, errors.New(ui.Plain(resp.Status))
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
