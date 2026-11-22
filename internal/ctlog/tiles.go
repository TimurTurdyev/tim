package ctlog

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// Без контакта в User-Agent часть журналов режет частоту; контакт tim не шлёт.
	userAgent = "tim"
	// Одновременных запросов на хост: Let's Encrypt за CDN, остальные держат общий предел на анонимов.
	leParallel    = 16
	otherParallel = 4
	tileWidth     = 256
	bodyLimit     = 4 << 20
	unpackLimit   = 16 << 20
	requestLimit  = 8 * time.Second
	// pingAfter: молчащее соединение проверяется и закрывается, иначе запросы на нём висят до своего срока.
	pingAfter    = 3 * time.Second
	errorRetries = 2
	busyRetries  = 7
	pauseMin     = 100 * time.Millisecond
	pauseMax     = 10 * time.Second
	gapMin       = 20 * time.Millisecond
	gapMax       = 250 * time.Millisecond
	maxSteps     = 48
)

var (
	// errNoTile: 403 и 404 у журналов значат "такой плитки нет", а не запрет.
	errNoTile   = errors.New("плитки имён нет")
	errNotInLog = errors.New("времени метки в журнале нет")
)

// web: запросы к журналам - свой транспорт, счёт объёма и очередь на каждый хост.
type web struct {
	client *http.Client
	limit  int64 // предел объёма по сети, байт
	stop   context.CancelFunc
	bytes  atomic.Int64
	tiles  atomic.Int64
	mu     sync.Mutex
	gates  map[string]*gate
}

// newWeb: stop зовётся, когда принято больше limit байт.
func newWeb(limit int64, stop context.CancelFunc) *web {
	dialer := &net.Dialer{Timeout: 8 * time.Second}
	return &web{
		client: &http.Client{Timeout: requestLimit, Transport: &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         dialer.DialContext,
			ForceAttemptHTTP2:   true,
			MaxIdleConnsPerHost: leParallel,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 8 * time.Second,
			HTTP2:               &http.HTTP2Config{SendPingTimeout: pingAfter, PingTimeout: pingAfter},
		}},
		limit: limit, stop: stop, gates: map[string]*gate{},
	}
}

func (w *web) close() { w.client.CloseIdleConnections() }

// gate: очередь хоста из адреса; parallel действует, когда хост встречен впервые.
func (w *web) gate(rawURL string, parallel int) *gate {
	host := rawURL
	if u, err := url.Parse(rawURL); err == nil {
		host = u.Host
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	g := w.gates[host]
	if g == nil {
		g = &gate{slots: make(chan struct{}, parallel)}
		w.gates[host] = g
	}
	return g
}

// get: распакованное тело и код ответа. 429 повторяется с растущей паузой на весь хост, 5xx и обрыв связи - дважды.
func (w *web) get(ctx context.Context, g *gate, rawURL string) ([]byte, int, error) {
	if err := g.enter(ctx); err != nil {
		return nil, 0, err
	}
	defer g.leave()
	busy, failed := 0, 0
	for {
		if err := g.wait(ctx); err != nil {
			return nil, 0, err
		}
		a, err := w.once(ctx, rawURL)
		switch {
		case err != nil && (ctx.Err() != nil || failed == errorRetries || timedOut(err)):
			return nil, 0, err
		case err != nil || a.status >= 500 && failed < errorRetries:
			failed++
			if err := sleep(ctx, time.Duration(failed)*500*time.Millisecond); err != nil {
				return nil, 0, err
			}
		case a.status == http.StatusTooManyRequests && busy == busyRetries:
			return nil, a.status, nil
		case a.status == http.StatusTooManyRequests:
			g.slow(time.Now().Add(retryPause(a.retry, busy, time.Now()) + rand.N(pauseMin)))
			busy++
		default:
			g.good()
			body, err := gunzip(a.data)
			return body, a.status, err
		}
	}
}

func (w *web) once(ctx context.Context, rawURL string) (answer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return answer{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	// свой Accept-Encoding выключает распаковку в net/http: так виден объём по сети, а сжатие узнаётся по сигнатуре
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := w.client.Do(req)
	if err != nil {
		return answer{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if w.bytes.Add(int64(len(data))) > w.limit {
		w.stop()
	}
	return answer{data: data, status: resp.StatusCode, retry: resp.Header.Get("Retry-After")}, err
}

// answer: ответ журнала как пришёл по сети.
type answer struct {
	data   []byte
	status int
	retry  string // заголовок Retry-After
}

// gate: очередь к одному хосту. После 429 между запросами появляется пауза, с чистыми ответами она спадает.
type gate struct {
	slots chan struct{}
	mu    sync.Mutex
	next  time.Time
	gap   time.Duration
	clean int // ответов подряд без 429
}

func (g *gate) enter(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case g.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *gate) leave() { <-g.slots }

func (g *gate) wait(ctx context.Context) error {
	g.mu.Lock()
	at := g.next
	if now := time.Now(); at.Before(now) {
		at = now
	}
	g.next = at.Add(g.gap)
	g.mu.Unlock()
	return sleep(ctx, time.Until(at))
}

func (g *gate) slow(until time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.gap, g.clean = min(max(g.gap*2, gapMin), gapMax), 0
	if until.After(g.next) {
		g.next = until
	}
}

func (g *gate) good() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.clean++; g.clean < 10 {
		return
	}
	g.gap, g.clean = g.gap*2/3, 0
	if g.gap < gapMin/2 {
		g.gap = 0
	}
}

// tile: разобранная плитка имён - сколько в ней записей, время первой и последней, имена домена.
type tile struct {
	count       int
	first, last int64 // мс
	hits        []hit
}

type hit struct {
	name string
	at   int64 // мс
}

// step: что делать дальше в поиске места по времени.
type step struct {
	kind  stepKind
	tiles []int64 // что прочитать или где нашлось
}

type stepKind int

const (
	stepRead stepKind = iota
	stepFound
	stepNone // времени в журнале нет
)

// retryPause: пауза после 429. Retry-After бывает датой с "UTC" и уже в прошлом, поэтому снизу своя растущая пауза.
func retryPause(header string, try int, now time.Time) time.Duration {
	floor := pauseMin << min(try, 4)
	wait := floor
	if sec, err := strconv.Atoi(header); err == nil {
		wait = time.Duration(sec) * time.Second
	} else if at, err := http.ParseTime(header); err == nil {
		wait = at.Sub(now)
	} else if at, err := time.Parse(time.RFC1123, header); err == nil {
		wait = at.Sub(now)
	}
	return min(max(wait, floor), pauseMax)
}

// timedOut: ответа не дождались; повтор завис бы так же и держал окно до конца общего срока.
func timedOut(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// gunzip распаковывает по сигнатуре: одни журналы жмут всегда, другие никогда, заголовок не показатель.
func gunzip(data []byte) ([]byte, error) {
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		return data, nil
	}
	z, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("распаковка ответа журнала: %w", err)
	}
	out, err := io.ReadAll(io.LimitReader(z, unpackLimit))
	if err != nil {
		return nil, fmt.Errorf("распаковка ответа журнала: %w", err)
	}
	return out, nil
}

// tilePath: путь плитки по номеру - тройки цифр, старшие с "x": 4733181 -> x004/x733/181.
func tilePath(n int64) string {
	path := fmt.Sprintf("%03d", n%1000)
	for n /= 1000; n > 0; n /= 1000 {
		path = fmt.Sprintf("x%03d/", n%1000) + path
	}
	return path
}

// parseTile: плитка имён - строка JSON на запись; целиком разбираются только строки с доменом.
func parseTile(body []byte, domain string) tile {
	const head = `{"timestamp":`
	var t tile
	for line := range bytes.Lines(bytes.ToLower(body)) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		t.count++
		at := int64(0)
		if rest, ok := bytes.CutPrefix(line, []byte(head)); ok {
			for _, c := range rest {
				if c < '0' || c > '9' {
					break
				}
				at = at*10 + int64(c-'0')
			}
		}
		if at == 0 || bytes.Contains(line, []byte(domain)) {
			var entry struct {
				Timestamp int64
				DNS       []string
			}
			if json.Unmarshal(line, &entry) == nil {
				at = entry.Timestamp
				for _, name := range entry.DNS {
					if name = hostName(name); inDomain(name, domain) {
						t.hits = append(t.hits, hit{name: name, at: at})
					}
				}
			}
		}
		if at <= 0 {
			continue
		}
		if t.first == 0 {
			t.first = at
		}
		t.last = at
	}
	return t
}

// parseCheckpoint: число записей журнала - вторая строка контрольной точки.
func parseCheckpoint(body []byte) (int64, error) {
	lines := strings.SplitN(string(body), "\n", 3)
	if len(lines) < 3 {
		return 0, errors.New("контрольная точка журнала короче трёх строк")
	}
	size, err := strconv.ParseInt(strings.TrimSpace(lines[1]), 10, 64)
	if err != nil || size < 0 {
		return 0, errors.New("в контрольной точке журнала нет числа записей")
	}
	return size, nil
}

// nextStep: шаг поиска времени at по парам "номер записи - время"; конец - запись не дальше tol от at.
func nextStep(points []point, at, tol int64, n int, last int64) step {
	i := sort.Search(len(points), func(i int) bool { return points[i].at > at })
	switch {
	case len(points) == 0:
		return step{kind: stepRead, tiles: []int64{0, last}}
	case i == 0 && points[0].pos == 0:
		return step{kind: stepNone}
	case i == 0:
		return step{kind: stepRead, tiles: []int64{0}}
	case i == len(points) && points[i-1].pos/tileWidth == last:
		return step{kind: stepFound, tiles: []int64{last}}
	case i == len(points):
		return step{kind: stepRead, tiles: []int64{last}}
	}
	lo, hi := points[i-1], points[i]
	switch {
	case at-lo.at <= tol || lo.pos/tileWidth == hi.pos/tileWidth || hi.pos-lo.pos <= 1:
		return step{kind: stepFound, tiles: []int64{lo.pos / tileWidth}}
	case hi.at-at <= tol:
		return step{kind: stepFound, tiles: []int64{hi.pos / tileWidth}}
	}
	return step{kind: stepRead, tiles: guesses(points, i, at, n)}
}

// guesses: где искать at между points[i-1] и points[i] - по вилке и по местной скорости; каждый четвёртый шаг пополам.
func guesses(points []point, i int, at int64, n int) []int64 {
	lo, hi := points[i-1], points[i]
	if n%4 == 3 {
		return []int64{(lo.pos + hi.pos) / 2 / tileWidth}
	}
	guess := (lo.pos + hi.pos) / 2
	if hi.at > lo.at {
		guess = lo.pos + int64(float64(at-lo.at)/float64(hi.at-lo.at)*float64(hi.pos-lo.pos))
	}
	out := []int64{min(max(guess, lo.pos+1), hi.pos-1) / tileWidth}
	local := int64(-1)
	switch {
	case at-lo.at <= hi.at-at && i >= 2:
		local = lo.pos + int64(float64(at-lo.at)*speed(points[i-2], lo))
	case at-lo.at > hi.at-at && i+1 < len(points):
		local = hi.pos - int64(float64(hi.at-at)*speed(hi, points[i+1]))
	}
	if local > lo.pos && local < hi.pos && local/tileWidth != out[0] {
		out = append(out, local/tileWidth)
	}
	return out
}

// speed: записей в мс между соседними известными записями; 0 - они слишком далеко друг от друга.
func speed(a, b point) float64 {
	if b.at <= a.at || b.pos-a.pos > 16*tileWidth {
		return 0
	}
	return float64(b.pos-a.pos) / float64(b.at-a.at)
}
