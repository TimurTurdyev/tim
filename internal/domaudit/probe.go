package domaudit

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strconv"
	"time"

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
}

// Silent: имя спрашивали по HTTP, и оно не ответило.
func (h Host) Silent() bool { return h.Status == 0 && !h.Unchecked }

// probe: обратные имена адресов и ответы сайтов. Главная страница домена читается тут же и целиком: счётчики
// берутся из неё, второй раз её не качаем. Имена сверх предела не спрашиваются: сам домен и www - в первую очередь.
func (r *Report) probe() {
	probes := par.NewGroup(probeWidth)
	for i := range r.Addrs {
		probes.Go(func() { r.Addrs[i].PTR = ptr(r.Addrs[i].IP) })
	}
	order := make([]int, len(r.Hosts))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(a, b int) int { return r.rank(a) - r.rank(b) })
	withAddr := 0
	for _, i := range order {
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
	got := askBoth(h.Name, limit)
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
	if home {
		h.trackers = Trackers(got.body)
	}
	if t, ok := MatchTakeover(h.CNAME, true, got.body); ok {
		h.Takeover = &t
	}
}

func askBoth(name string, limit int64) *answer {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	client := webnet.Fresh(probeTimeout)
	plain := make(chan *answer, 1)
	go func() { plain <- ask(ctx, client, "http", name, limit) }()
	if got := ask(ctx, client, "https", name, limit); got != nil {
		return got
	}
	return <-plain
}

func ask(ctx context.Context, client *http.Client, scheme, name string, limit int64) *answer {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, scheme+"://"+name, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh) tim")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, limit))
	got := &answer{scheme: scheme, status: resp.StatusCode, server: ui.Plain(resp.Header.Get("Server")), body: string(body)}
	// Редирект отмечаем, только если ушли на другой адрес, а не просто на "/" того же имени.
	u := resp.Request.URL
	if u.Host != name || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		got.final = u.String()
	}
	return got
}
