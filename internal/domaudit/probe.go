package domaudit

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/webnet"
)

const (
	probeTimeout = 5 * time.Second // на имя целиком: https и http вместе
	probeWidth   = 32
	maxProbes    = 300
	pageLimit    = 256 << 10
	homeLimit    = 512 << 10
)

// answer: что ответил сайт по одной схеме; nil - не ответил.
type answer struct {
	scheme string
	status int
	server string
	final  string
	body   string
	cert   *x509.Certificate
}

// Silent: имя спрашивали по HTTP, и оно не ответило.
func (h Host) Silent() bool { return h.Status == 0 && !h.Unchecked && !h.Internal }

// probe: ответы сайтов. Главная страница домена читается тут же и целиком: счётчики берутся из неё, второй
// раз её не качаем. Имена сверх предела и внутренние не спрашиваются: сам домен и www - в первую очередь.
func (r *Report) probe() {
	probes := par.NewGroup(probeWidth)
	order := make([]int, len(r.Hosts))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return r.rank(a) - r.rank(b) })
	withAddr := 0
	for _, i := range order {
		if r.Hosts[i].Internal {
			continue
		}
		if len(r.Hosts[i].IPs) > 0 {
			withAddr++
			if withAddr > maxProbes {
				r.Hosts[i].Unchecked = true
				continue
			}
		}
		probes.Go(func() { probe(&r.Hosts[i], r.rank(i) < 2) })
	}
	probes.Wait()
	if withAddr > maxProbes {
		r.Notes = append(r.Notes, "по HTTP проверено "+strconv.Itoa(maxProbes)+" имён из "+strconv.Itoa(withAddr))
	}
}

// rank: сам домен, www, остальные - по алфавиту, как в списке.
func (r *Report) rank(i int) int {
	switch r.Hosts[i].Name {
	case r.Domain:
		return 0
	case "www." + r.Domain:
		return 1
	}
	return 2
}

// probe: что отвечает имя. https и http спрашиваются разом, ответ https важнее; у главной страницы - ещё и счётчики.
func probe(h *Host, home bool) {
	limit := int64(pageLimit)
	if home {
		limit = homeLimit
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	got, certError := askBoth(ctx, h.Name, limit)
	if got == nil && len(h.IPs) > 0 && ctx.Err() == nil {
		// Быстрый обрыв бывает и случайным, а молчащее имя советуют удалить: спрашиваем ещё раз.
		got, certError = askBoth(ctx, h.Name, limit)
	}
	h.CertError = certError
	if got == nil {
		if len(h.IPs) == 0 {
			if t, ok := MatchTakeover(h.CNAME, false, ""); ok {
				h.Takeover = &t
			}
		}
		return
	}
	h.Scheme, h.Status, h.Server, h.Final = got.scheme, got.status, got.server, got.final
	h.Title = Title(got.body)
	if got.cert != nil {
		h.CertName, h.certNames = certNames(got.cert)
	}
	if home {
		h.trackers = Trackers(got.body)
	}
	if t, ok := MatchTakeover(h.CNAME, true, got.body); ok {
		h.Takeover = &t
	}
}

// askBoth: ответ по https, иначе по http. Сертификат не прошёл проверку - сайт всё равно жив: если и http
// молчит, страница читается без проверки; причина возвращается словами.
func askBoth(ctx context.Context, name string, limit int64) (got *answer, certError string) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client := webnet.Fresh(probeTimeout)
	plain, unchecked := make(chan *answer, 1), make(chan *answer, 1)
	go func() {
		a, _ := ask(ctx, client, "http", name, limit)
		plain <- a
	}()
	got, err := ask(ctx, client, "https", name, limit)
	if err == nil {
		return got, ""
	}
	certError, cert := certTrouble(name, err)
	if certError != "" {
		go func() {
			a, _ := ask(ctx, webnet.Client(probeTimeout, true), "https", name, limit)
			unchecked <- a
		}()
	}
	if got = <-plain; got == nil && certError != "" {
		got = <-unchecked
	}
	if got != nil && got.cert == nil {
		got.cert = cert
	}
	return got, certError
}

func ask(ctx context.Context, client *http.Client, scheme, name string, limit int64) (*answer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+name, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) tim")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, limit))
	got := &answer{scheme: scheme, status: resp.StatusCode, server: ui.Plain(resp.Header.Get("Server")), body: string(body)}
	// Редирект отмечаем, только если ушли на другой адрес, а не просто на "/" того же имени.
	u := resp.Request.URL
	if u.Host != name || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		got.final = u.String()
	}
	if first := firstHop(resp); first.TLS != nil && len(first.TLS.PeerCertificates) > 0 {
		got.cert = first.TLS.PeerCertificates[0]
	}
	return got, nil
}

// firstHop: ответ самого имени - до перехода по редиректам.
func firstHop(resp *http.Response) *http.Response {
	for resp.Request != nil && resp.Request.Response != nil {
		resp = resp.Request.Response
	}
	return resp
}

// certTrouble: почему сертификат не прошёл проверку и сам сертификат; не у имени, а там, куда оно
// перенаправляет, - причина с именем того сайта.
func certTrouble(name string, err error) (reason string, cert *x509.Certificate) {
	certErr, ok := errors.AsType[*tls.CertificateVerificationError](err)
	if !ok {
		return "", nil
	}
	reason = inspect.CertReason(certErr)
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		if u, err := url.Parse(urlErr.URL); err == nil && u.Hostname() != name {
			return u.Hostname() + ": " + reason, nil
		}
	}
	if len(certErr.UnverifiedCertificates) > 0 {
		cert = certErr.UnverifiedCertificates[0]
	}
	return reason, cert
}

// certNames: главное имя сертификата и все его имена.
func certNames(cert *x509.Certificate) (main string, all []string) {
	for _, n := range cert.DNSNames {
		all = append(all, ui.Plain(n))
	}
	main = ui.Plain(cert.Subject.CommonName)
	if main == "" && len(all) > 0 {
		main = all[0]
	}
	return main, all
}
