package domaudit

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/ctlog"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	crtTimeout    = 45 * time.Second
	sourceTimeout = 15 * time.Second
	sourceWait    = 20 * time.Second // дольше сбор источник не ждёт; поздний ответ ляжет в кэш
	sourceLimit   = 32 << 20
	thcLimit      = 1000 // больше строк не просим: ровно столько - ответ обрезан
	hostLimit     = 50   // столько строк отдаёт hackertarget без ключа
)

var (
	errNotJSON = errors.New("ответил не JSON")
	errSlow    = errors.New("не ответил вовремя")
	errNoQuota = errors.New("дневной лимит бесплатного сервиса исчерпан")
)

// sources: откуда берутся имена - по важности: имя подписывается первым, кто его знает. CertSpotter быстрый,
// crt.sh глубже по истории, остальные видят то, чего нет в сертификатах.
var sources = []source{
	{name: "CertSpotter", label: fromCert, fetch: fetchCertSpotter},
	{name: "crt.sh", label: fromCert, ttl: namesTTL, fetch: fetchCRT},
	{name: "THC", label: "THC", ttl: namesTTL, fetch: fetchTHC},
	{name: "subdomain.center", label: "subdomain.center", ttl: namesTTL, fetch: fetchSubdomainCenter},
	{name: "hackertarget", label: "hackertarget", ttl: namesTTL, quota: true, fetch: fetchHostSearch},
}

// source: источник имён. label - чем подписать имя; ttl - сколько верить своему кэшу, 0 - кэш у самого источника;
// quota - дневной лимит общий с соседями, без соседей его не тратим.
type source struct {
	name, label string
	ttl         time.Duration
	quota       bool
	fetch       func(domain string) (nameList, error)
}

// SourceState: что дал источник имён. Cached - список из кэша, тогда и получен; Cut - источник отдал не всё.
type SourceState struct {
	Name   string    `json:"name"`
	Names  int       `json:"names"`
	Cached time.Time `json:"cached,omitzero"`
	Cut    bool      `json:"cut,omitempty"`
	Error  string    `json:"error,omitempty"`
}

// found: имена всех источников - кто какое знает, по порядку sources; related - другие домены тех же сертификатов.
type found struct {
	by      map[string][]string // имя -> источники
	label   map[string]string   // имя -> подпись первого источника
	related []string
	states  []SourceState
}

// askSources опрашивает источники разом; свежее берётся из кэша, молчащему - прошлый список.
func askSources(domain string, opt Options) found {
	list := activeSources(opt)
	type answer struct {
		got   nameList
		state SourceState
	}
	answers := par.Map(list, len(list), func(s source) answer {
		got, state := s.ask(domain, opt.FreshCRT)
		return answer{got, state}
	})
	f := found{by: map[string][]string{}, label: map[string]string{}}
	for i, a := range answers {
		f.states = append(f.states, a.state)
		f.related = append(f.related, a.got.Related...)
		for _, n := range a.got.Subs {
			f.by[n] = append(f.by[n], list[i].name)
			if f.label[n] == "" {
				f.label[n] = list[i].label
			}
		}
	}
	slices.Sort(f.related)
	f.related = slices.Compact(f.related)
	return f
}

// stale: какие источники придётся спросить в сети - для строки хода; у кого кэш свой, те не в счёт.
func stale(domain string, opt Options) []string {
	var out []string
	for _, s := range activeSources(opt) {
		if _, ok := s.cached(domain); s.ttl > 0 && (!ok || opt.FreshCRT) {
			out = append(out, s.name)
		}
	}
	return out
}

func activeSources(opt Options) []source {
	return slices.DeleteFunc(slices.Clone(sources), func(s source) bool { return s.quota && opt.SkipNeighbors })
}

// ask: имена источника и что о них сказать.
func (s source) ask(domain string, fresh bool) (nameList, SourceState) {
	state := SourceState{Name: s.name}
	old, has := loadNames(s.name, domain)
	if c, ok := s.cached(domain); ok && !fresh {
		state.Names, state.Cut, state.Cached, state.Error = len(c.Subs), c.Cut, c.Got, c.Error
		return c, state
	}
	got, err := s.wait(domain)
	if err != nil {
		// Прошлый список лучше пустого; ошибка запоминается, чтобы не ждать молчащего при каждом запуске.
		got = nameList{Error: ui.Plain(ui.ShortErr(netError(err)))}
		if has {
			got.Subs, got.Related, got.Cut, got.Got = old.Value.Subs, old.Value.Related, old.Value.Cut, old.Value.Got
		}
		state.Cached = got.Got
		storeNames(s.name, domain, got)
	}
	state.Names, state.Cut, state.Error = len(got.Subs), got.Cut, got.Error
	return got, state
}

// wait: ответ источника или отказ, если тот думает слишком долго; ответ ложится в кэш, даже если его не дождались.
func (s source) wait(domain string) (nameList, error) {
	type answer struct {
		got nameList
		err error
	}
	done := make(chan answer, 1)
	go func() {
		got, err := s.fetch(domain)
		if err == nil {
			got.Got = time.Now()
			storeNames(s.name, domain, got)
		}
		done <- answer{got, err}
	}()
	select {
	case a := <-done:
		return a.got, a.err
	case <-time.After(sourceWait):
		return nameList{}, errSlow
	}
}

// cached: список из кэша, которому ещё можно верить; после отказа - недолго.
func (s source) cached(domain string) (nameList, bool) {
	e, ok := loadNames(s.name, domain)
	if !ok || s.ttl == 0 {
		return nameList{}, false
	}
	ttl := s.ttl
	if e.Value.Error != "" {
		ttl = retryTTL
	}
	return e.Value, e.Fresh(ttl)
}

func fetchCertSpotter(domain string) (nameList, error) {
	list, err := ctlog.Issuances(domain)
	if err != nil {
		return nameList{}, err
	}
	var all []string
	for _, c := range list {
		all = append(all, c.Names...)
	}
	subs, related := splitNames(all, domain)
	return nameList{Subs: subs, Related: related}, nil
}

func fetchCRT(domain string) (nameList, error) {
	r, err := get("https://crt.sh/?q="+url.QueryEscape("%."+domain)+"&output=json", crtTimeout)
	if err != nil {
		return nameList{}, err
	}
	if !bytes.HasPrefix(bytes.TrimSpace(r.body), []byte("[")) {
		return nameList{}, errNotJSON
	}
	subs, related, err := ParseCRT(r.body, domain)
	return nameList{Subs: subs, Related: related}, err
}

func fetchTHC(domain string) (nameList, error) {
	r, err := get("https://ip.thc.org/api/v1/subdomains/download?domain="+url.QueryEscape(domain)+"&limit=1000&hide_header=true", sourceTimeout)
	if err != nil {
		return nameList{}, err
	}
	subs, rows := ParseTHC(string(r.body), domain)
	return nameList{Subs: subs, Cut: rows >= thcLimit}, nil
}

func fetchSubdomainCenter(domain string) (nameList, error) {
	r, err := get("https://api.subdomain.center/?domain="+url.QueryEscape(domain), sourceTimeout)
	if err != nil && r.status != http.StatusBadRequest {
		return nameList{}, err
	}
	if r.status == http.StatusBadRequest { // так он отвечает на зону, которой не знает
		return nameList{}, nil
	}
	subs, err := ParseSubdomainCenter(r.body, domain)
	return nameList{Subs: subs, Cut: r.header.Get("X-Truncated") == "true"}, err
}

func fetchHostSearch(domain string) (nameList, error) {
	r, err := get("https://api.hackertarget.com/hostsearch/?q="+url.QueryEscape(domain), sourceTimeout)
	if err != nil {
		return nameList{}, err
	}
	subs, rows, err := ParseHostSearch(string(r.body), domain)
	return nameList{Subs: subs, Cut: rows >= hostLimit}, err
}

// reply: ответ источника.
type reply struct {
	status int
	header http.Header
	body   []byte
}

func get(target string, timeout time.Duration) (reply, error) {
	resp, err := (&http.Client{Timeout: timeout}).Get(target)
	if err != nil {
		return reply{}, netError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	r := reply{status: resp.StatusCode, header: resp.Header}
	if r.body, err = io.ReadAll(io.LimitReader(resp.Body, sourceLimit)); err != nil {
		return r, netError(err)
	}
	if strings.Contains(string(r.body[:min(len(r.body), 200)]), "API count exceeded") {
		return r, errNoQuota
	}
	if r.status != http.StatusOK {
		return r, errors.New("ответил " + ui.Plain(resp.Status))
	}
	return r, nil
}

// netError: ошибка сети без адреса запроса - в нём повторяется домен.
func netError(err error) error {
	if u, ok := errors.AsType[*url.Error](err); ok {
		if u.Timeout() {
			return errSlow
		}
		return u.Err
	}
	return err
}
