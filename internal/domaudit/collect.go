package domaudit

import (
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/asn"
	"github.com/TimurTurdyev/tim/internal/par"
)

// Откуда имя, если не из архива: подпись источника.
const (
	fromApex  = "апекс"
	fromCert  = "сертификат"
	fromBrute = "перебор"
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
	// Sources: все источники, которые знают имя.
	Sources []string `json:"sources,omitempty"`
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
	// Sources: что дал каждый источник имён.
	Sources []SourceState `json:"sources,omitempty"`
	Notes   []string      `json:"notes,omitempty"` // что осталось за пределами сбора
	// CRTCached: не пусто, если список из логов сертификатов взят из кэша.
	CRTCached time.Time `json:"crt_cached,omitzero"`

	by   map[string][]string // имя -> источники, которые его знают
	wild []string            // адреса несуществующего имени
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

// discover: имена домена из источников и перебора - откуда каждое взялось, и адреса тех, что уже резолвились
// при переборе.
func (r *Report) discover(opt Options) (label map[string]string, known map[string][]string) {
	f := found{}
	par.All(func() {
		if opt.Quiet {
			return
		}
		if opt.Notify != nil {
			opt.Notify(sourcesNote(stale(r.Domain, opt)))
		}
		f = askSources(r.Domain, opt)
	}, func() {
		r.wild = wildcard(r.Domain)
		known = bruteforce(r.Domain, r.wild)
	})
	r.Related, r.Sources, r.by = f.related, f.states, f.by
	failed := 0
	for _, s := range f.states {
		if s.Name == "crt.sh" {
			r.CRTCached = s.Cached
		}
		if s.Error != "" && s.Names == 0 {
			failed++
		}
	}
	if failed > 0 && failed == len(f.states) {
		r.Errors = append(r.Errors, "ни один источник имён не ответил: имена только из перебора частых")
	}
	label = map[string]string{r.Domain: fromApex}
	for _, names := range []map[string]string{f.label, brute(known)} {
		for n, from := range names {
			if label[n] == "" {
				label[n] = from
			}
		}
	}
	return label, known
}

func brute(known map[string][]string) map[string]string {
	out := map[string]string{}
	for n := range known {
		out[n] = fromBrute
	}
	return out
}

func sourcesNote(asked []string) string {
	if len(asked) == 0 {
		return "имена из кэша, обновить: --fresh"
	}
	return "спрашиваю имена: " + strings.Join(asked, ", ")
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
