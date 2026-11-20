package sweep

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// idle: имени для запроса сейчас нет
const idle int32 = -1

const (
	dohTimeout = 8 * time.Second
	// dohFirst: срок одиночного запроса - он один на соединении и ждать его дольше незачем
	dohFirst = 4 * time.Second
	// dohPause: после отказа или сбоя поток ждёт, а имя повторяется не раньше, чем через столько
	dohPause = time.Second
	// dohStrikes: столько одиночных запросов подряд без ответа - адрес бросаем и идём к запасному
	dohStrikes   = 3
	dohWidth     = 16
	dohBody      = 64 << 10
	dohMinWait   = 100 * time.Millisecond
	dohIdle      = 20 * time.Millisecond
	dohHandshake = 8 * time.Second
)

// dohWidths: сколько запросов сразу резолвер держит по одному соединению без отказов.
var dohWidths = map[string]int{"dns.google": 64, "cloudflare-dns.com": 16}

// later: имя и время, раньше которого его не повторять.
type later struct {
	idx int32
	due time.Duration
}

// desk: раздача имён потокам DoH - повторы после паузы раньше новых имён, новые - пока позволяет срок.
type desk struct {
	delay
	book     ledger
	retry    []later
	deadline time.Duration
	// cold: связь рвалась - запросы идут по одному, пока резолвер не ответит: иначе транспорт
	// откроет по соединению на каждый ждущий запрос
	cold bool
	// lone: имя одиночного запроса в пути
	lone int32
	// rest: раньше следующий одиночный запрос не уходит
	rest       time.Duration
	strikes    int
	progressAt time.Duration
}

func newDesk(names []string, deadline time.Duration) desk {
	return desk{book: newLedger(names), deadline: deadline, lone: idle}
}

// warm: новый адрес ответил на одиночный запрос - раздача идёт во всю ширину.
func (d *desk) warm(now time.Duration) {
	d.cold, d.lone, d.strikes, d.progressAt = false, idle, 0, now
}

func (d *desk) broken() bool { return d.strikes >= dohStrikes }

func (d *desk) stopNew(now time.Duration) bool {
	return d.deadline > 0 && d.deadline-now < d.limit(dohMinWait, dohMinWait)+stopMargin
}

func (d *desk) finished(now time.Duration) bool {
	switch {
	case d.deadline > 0 && now >= d.deadline:
		return true
	case d.book.open == 0 && (d.stopNew(now) || !d.book.left()):
		return true
	}
	return now-d.progressAt > stallLimit
}

// next: имя для запроса; idle - дать пока нечего, заглянуть через wait.
func (d *desk) next(now time.Duration) (idx int32, wait time.Duration) {
	if d.cold && (d.lone != idle || now < d.rest) {
		return idle, dohIdle
	}
	idx, wait = d.pick(now)
	if d.cold {
		d.lone = idx
	}
	return idx, wait
}

func (d *desk) pick(now time.Duration) (idx int32, wait time.Duration) {
	if len(d.retry) > 0 && d.retry[0].due <= now {
		idx = d.retry[0].idx
		d.retry = d.retry[1:]
		d.book.state[idx] = flying
		d.book.tries[idx]++
		return idx, 0
	}
	if !d.stopNew(now) {
		if idx, ok := d.book.issue(); ok {
			d.book.tries[idx]++
			return idx, 0
		}
	}
	if len(d.retry) > 0 {
		return idle, min(d.retry[0].due-now, dohPause)
	}
	return idle, dohIdle
}

// take: итог запроса; reached - резолвер ответил сообщением DNS. true - отказ или сбой, имя уйдёт на повтор позже.
func (d *desk) take(idx int32, a Answer, reached bool, took, now time.Duration) bool {
	d.link(idx, reached, now)
	if definite(a.Kind) {
		// отказы в оценку не идут: резолвер присылает их через секунды
		d.sample(took)
		if d.book.settle(idx, a) {
			d.progressAt = now
		}
		return false
	}
	if a.Kind == Fail {
		d.book.noteFail(idx)
	}
	if int(d.book.tries[idx]) >= triesResolver {
		d.book.drop(idx)
		return true
	}
	d.book.state[idx] = queued
	d.retry = append(d.retry, later{idx: idx, due: now + dohPause})
	return true
}

// link: что запрос говорит о связи. Сбой десятков запросов одного соединения - один сбой, а не десятки.
func (d *desk) link(idx int32, reached bool, now time.Duration) {
	lone := d.cold && idx == d.lone
	if lone {
		d.lone = idle
	}
	switch {
	case reached:
		d.cold, d.strikes = false, 0
	case !d.cold:
		d.cold, d.rest = true, now+dohPause
	case lone:
		d.strikes++
		d.rest = now + dohPause
	}
}

// secure: перебор по DoH - одно соединение HTTP/2 и столько потоков, сколько держит резолвер.
type secure struct {
	opt    Options
	client *http.Client
	start  time.Time
	mu     sync.Mutex
	desk   desk
}

func overHTTPS(ctx context.Context, names []string, opt Options, start time.Time) Result {
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		DialContext:         (&net.Dialer{Timeout: dialTimeout, Control: opt.Control}).DialContext,
		ForceAttemptHTTP2:   true,
		TLSHandshakeTimeout: dohHandshake,
	}
	defer transport.CloseIdleConnections()
	s := &secure{opt: opt, client: &http.Client{Transport: transport}, start: start}
	s.desk = newDesk(names, horizon(ctx, start, opt.Budget))
	if s.desk.deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, start.Add(s.desk.deadline))
		defer cancel()
	}
	for _, address := range opt.DoH {
		// без первого одиночного запроса транспорт открывает по соединению на каждый ждущий запрос
		if _, reached := s.ask(ctx, address, probeName(names), dohFirst); !reached {
			continue
		}
		if s.pool(ctx, address) {
			break
		}
	}
	s.report()
	return s.desk.book.result()
}

// pool: потоки на один адрес; false - адрес перестал отвечать, нужен запасной.
func (s *secure) pool(ctx context.Context, address string) bool {
	s.mu.Lock()
	s.desk.warm(time.Since(s.start))
	s.mu.Unlock()
	var wg sync.WaitGroup
	for range widthOf(address) {
		wg.Go(func() { s.work(ctx, address) })
	}
	drained := make(chan struct{})
	go func() {
		wg.Wait()
		close(drained)
	}()
	ticker := time.NewTicker(progressEvery)
	defer ticker.Stop()
	for {
		select {
		case <-drained:
			return !s.broken()
		case <-ticker.C:
			s.report()
		}
	}
}

func (s *secure) work(ctx context.Context, address string) {
	for ctx.Err() == nil {
		began := time.Since(s.start)
		idx, wait, timeout := idle, dohIdle, dohTimeout
		s.mu.Lock()
		over := s.desk.finished(began) || s.desk.broken()
		if !over {
			idx, wait = s.desk.next(began)
		}
		if s.desk.cold {
			timeout = dohFirst
		}
		s.mu.Unlock()
		switch {
		case over:
			return
		case idx == idle:
			pause(ctx, wait)
			continue
		}
		a, reached := s.ask(ctx, address, s.desk.book.names[idx], timeout)
		if ctx.Err() != nil {
			return
		}
		now := time.Since(s.start)
		s.mu.Lock()
		again := s.desk.take(idx, a, reached, now-began, now)
		s.mu.Unlock()
		if !reached {
			// мёртвое соединение уходит из запаса транспорта, иначе одиночный запрос попадёт в него же
			s.client.CloseIdleConnections()
		}
		if again {
			pause(ctx, dohPause)
		}
	}
}

// ask: один вопрос по RFC 8484, номер запроса 0; reached - резолвер ответил сообщением DNS.
func (s *secure) ask(ctx context.Context, address, name string, timeout time.Duration) (a Answer, reached bool) {
	a.Name = name
	msg, err := buildQuery(nil, 0, name, s.opt.Type, true)
	if err != nil {
		return a, false
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(msg))
	if err != nil {
		return a, false
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	req.Header.Set("User-Agent", "tim")
	resp, err := s.client.Do(req)
	if err != nil {
		return a, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, dohBody))
	_ = resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		return a, false
	}
	a, truncated := Classify(body, name)
	if a.Kind == None && !truncated {
		return a, false
	}
	// обрезанному ответу по HTTPS взяться неоткуда: считаем отказом и повторяем
	if truncated {
		a.Kind = Fail
	}
	return a, true
}

func (s *secure) broken() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.desk.broken()
}

func (s *secure) report() {
	if s.opt.Progress == nil {
		return
	}
	s.mu.Lock()
	done, total := s.desk.book.progress()
	s.mu.Unlock()
	s.opt.Progress(done, total)
}

func widthOf(address string) int {
	u, err := url.Parse(address)
	if err != nil {
		return dohWidth
	}
	if w, ok := dohWidths[u.Hostname()]; ok {
		return w
	}
	return dohWidth
}

func pause(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
