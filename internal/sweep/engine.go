package sweep

import "time"

// probe: запрос-проба молчащему серверу - имени за ним нет
const probe int32 = -1

const (
	triesZone     = 4
	triesResolver = 6
	// retryAhead: среди стольких первых повторов ищется имя, которое сервер ещё не спрашивал
	retryAhead = 8
	// retryQueue: столько повторов на сервер в очереди - и новые имена не выдаются
	retryQueue = 4
	maxPeers   = 16
	ringSize   = 1 << 16
	tcpWindow  = 64
)

type slotState uint8

const (
	slotFree slotState = iota
	slotLive           // ждёт ответа
	slotLost           // срок вышел
	slotDone           // ответ пришёл
)

// slot: запрос по его номеру - ответ принимается, только если совпали номер и имя вопроса.
type slot struct {
	at    time.Duration
	idx   int32
	state slotState
}

// flight: запрос в пути; лежат по порядку отправки - так проверяются сроки.
type flight struct {
	at  time.Duration
	idx int32
	id  uint16
}

// shot: что отправить - имя idx или пробу серверу server под номером id.
type shot struct {
	server int
	idx    int32
	id     uint16
}

type peer struct {
	pace    pacer
	ring    []slot
	next    uint16
	flights []flight
	head    int
	// когда сервер в последний раз ответил чем угодно и когда - обрезанным ответом; -1 - ещё ни разу
	lastReply, lastClip time.Duration
	// tcp: имена с обрезанным ответом ждут очереди по TCP
	tcp     []int32
	tcpDown bool
}

// engine: перебор без сокетов и часов - ему говорят время и ответы, он решает, что и кому отправить.
type engine struct {
	book  ledger
	peers []*peer
	// root: общий родитель имён - о нём спрашивает проба
	root string
	// по именам: когда и кому ушла последняя попытка
	sentAt []time.Duration
	sentTo []uint8
	retry  []int32
	tries  int
	tokens float64
	last   time.Duration
	turn   int
	// deadline: срок от начала; 0 - срока нет
	deadline   time.Duration
	stopNew    bool
	progressAt time.Duration
	out        []shot
}

func newEngine(names []string, servers int, recursive bool, deadline time.Duration) *engine {
	lim, tries := limits{start: startRate, floor: floorRate, top: serverTop, minTimeout: minTimeout}, triesZone
	if recursive {
		lim.start, lim.minTimeout, tries = startRateResolver, minTimeoutResolver, triesResolver
	}
	e := &engine{book: newLedger(names), root: probeName(names), tries: tries, deadline: deadline,
		sentAt: make([]time.Duration, len(names)), sentTo: make([]uint8, len(names))}
	for range servers {
		pace := newPacer(lim, totalTop/float64(servers))
		e.peers = append(e.peers, &peer{pace: pace, ring: make([]slot, ringSize), lastReply: -1, lastClip: -1})
	}
	return e
}

// tick: шаг движка - сроки запросов, решения о частоте, раздача. Возвращает, что отправить.
func (e *engine) tick(now time.Duration) []shot {
	e.out = e.out[:0]
	var longest time.Duration
	for _, p := range e.peers {
		e.expire(p, now)
		p.pace.step(now)
		if !p.pace.silent {
			longest = max(longest, p.pace.timeout())
		}
	}
	if longest == 0 {
		longest = firstTimeout
	}
	// повторы должны успеть вернуться до конца срока; на коротком сроке под них уходит не больше половины
	if e.deadline > 0 && e.deadline-now < min(longest, e.deadline/2)+stopMargin {
		e.stopNew = true
	}
	seconds := (now - e.last).Seconds()
	e.last = now
	e.tokens = min(e.tokens+totalTop*seconds, totalTop*burst+1)
	for _, p := range e.peers {
		p.pace.refill(seconds)
	}
	e.deal(now)
	return e.out
}

func (e *engine) finished(now time.Duration) bool {
	switch {
	case e.deadline > 0 && now >= e.deadline:
		return true
	case e.book.open == 0 && (e.stopNew || !e.book.left()):
		return true
	}
	return now-e.progressAt > stallLimit
}

// result: ограничение частоты называет правило частоты - одиночный обрезанный ответ бывает и просто большим.
func (e *engine) result() Result {
	res := e.book.result()
	for _, p := range e.peers {
		res.Limited = res.Limited || p.pace.limited
	}
	return res
}

func (e *engine) name(idx int32) string {
	if idx == probe {
		return e.root
	}
	return e.book.names[idx]
}

// expire: запросы, на которые срок ответа вышел, - потеря для частоты и повтор для имени.
func (e *engine) expire(p *peer, now time.Duration) {
	timeout := p.pace.timeout()
	for p.head < len(p.flights) && p.flights[p.head].at+timeout <= now {
		f := p.flights[p.head]
		p.head++
		s := &p.ring[f.id]
		lost := s.state == slotLive && s.at == f.at
		if lost {
			s.state = slotLost
			p.pace.onLoss(f.at)
		}
		if f.idx == probe || e.book.state[f.idx] != flying || e.sentAt[f.idx] != f.at {
			continue
		}
		switch {
		case lost && p.lastClip >= f.at && !p.tcpDown:
			// ограничитель частоты половину отказов обрезает, половину глотает: тишина рядом с обрезанным - тот же отказ
			e.toTCP(p, f.idx)
			continue
		case lost && p.lastReply < f.at:
			// сервер молчал всё время после запроса: это не про имя, попытку ему возвращаем
			e.book.tries[f.idx]--
		}
		e.again(f.idx)
	}
	if p.head > 4096 {
		p.flights = append(p.flights[:0], p.flights[p.head:]...)
		p.head = 0
	}
}

// again: имя идёт на повтор, а если попытки кончились - бросается.
func (e *engine) again(idx int32) {
	if int(e.book.tries[idx]) >= e.tries {
		e.book.drop(idx)
		return
	}
	e.book.state[idx] = queued
	e.retry = append(e.retry, idx)
}

// deal: раздача запросов по кругу, пока есть жетоны; молчащему серверу - только проба.
func (e *engine) deal(now time.Duration) {
	healthy := 0
	for _, p := range e.peers {
		if !p.pace.silent {
			healthy++
		}
	}
	for sent := true; sent && e.tokens >= 1; {
		sent = false
		for k := range e.peers {
			n := (e.turn + k) % len(e.peers)
			p := e.peers[n]
			if p.pace.tokens < 1 || e.tokens < 1 {
				continue
			}
			if p.pace.silent {
				e.fire(n, probe, now)
				continue
			}
			idx, ok := e.pick(n, healthy)
			if !ok {
				continue
			}
			e.fire(n, idx, now)
			e.tokens--
			sent = true
		}
	}
	e.turn++
}

// pick: имя для сервера n - повторы раньше новых и по возможности к другому серверу.
func (e *engine) pick(n, healthy int) (int32, bool) {
	for len(e.retry) > 0 && e.book.state[e.retry[0]] != queued {
		e.retry = e.retry[1:]
	}
	crowded := len(e.retry) >= retryQueue*len(e.peers)
	same := -1
	for k := 0; k < min(len(e.retry), retryAhead); k++ {
		idx := e.retry[k]
		switch {
		case e.book.state[idx] != queued:
		case int(e.sentTo[idx]) != n:
			return e.takeRetry(k), true
		case same < 0:
			same = k
		}
	}
	// тому же серверу повтор идёт, только если больше некому или повторы копятся
	if same >= 0 && (healthy < 2 || crowded) {
		return e.takeRetry(same), true
	}
	if e.stopNew || crowded {
		return 0, false
	}
	return e.book.issue()
}

func (e *engine) takeRetry(k int) int32 {
	idx := e.retry[k]
	e.retry = append(e.retry[:k], e.retry[k+1:]...)
	return idx
}

func (e *engine) fire(n int, idx int32, now time.Duration) {
	p := e.peers[n]
	id := p.next
	p.next++
	p.ring[id] = slot{at: now, idx: idx, state: slotLive}
	p.flights = append(p.flights, flight{at: now, idx: idx, id: id})
	p.pace.onSend()
	if idx != probe {
		e.book.state[idx] = flying
		e.book.tries[idx]++
		e.sentAt[idx], e.sentTo[idx] = now, uint8(n)
	}
	e.out = append(e.out, shot{server: n, idx: idx, id: id})
}

// receive: сообщение от сервера n - ответ принимается, только если совпали номер и имя вопроса.
func (e *engine) receive(n int, msg []byte, now time.Duration) {
	id, asked, ok := question(msg)
	if !ok {
		return
	}
	p := e.peers[n]
	s := &p.ring[id]
	if s.state == slotFree || s.state == slotDone || !sameName(asked, e.name(s.idx)) {
		return
	}
	a, truncated := Classify(msg, e.name(s.idx))
	good := definite(a.Kind)
	switch {
	case s.state == slotLive:
		p.pace.onReply(s.at, now, good, truncated)
	case good:
		p.pace.onLate(s.at, now)
	}
	s.state, p.lastReply = slotDone, now
	if truncated {
		p.lastClip = now
	}
	if s.idx != probe {
		e.land(p, s, a, truncated, now)
	}
}

// land: что ответ значит для имени.
func (e *engine) land(p *peer, s *slot, a Answer, truncated bool, now time.Duration) {
	idx := s.idx
	if definite(a.Kind) {
		if e.book.settle(idx, a) {
			e.progressAt = now
		}
		return
	}
	if a.Kind == Fail {
		e.book.noteFail(idx)
	}
	// ответ на прошлую попытку не в счёт: имя уже ждёт другого сервера
	if truncated && !p.tcpDown && e.book.state[idx] == flying && e.sentAt[idx] == s.at {
		e.toTCP(p, idx)
	}
}

func (e *engine) toTCP(p *peer, idx int32) {
	e.book.state[idx] = viaTCP
	p.tcp = append(p.tcp, idx)
}

// tcpWaiting: у сервера n есть имена, которые пора спросить по TCP.
func (e *engine) tcpWaiting(n int) bool { return len(e.peers[n].tcp) > 0 }

// takeTCP: имена сервера n, которые пора спросить по TCP, - не больше limit.
func (e *engine) takeTCP(n, limit int) []int32 {
	p := e.peers[n]
	var batch []int32
	for len(p.tcp) > 0 && len(batch) < limit {
		idx := p.tcp[0]
		p.tcp = p.tcp[1:]
		if e.book.state[idx] == viaTCP {
			batch = append(batch, idx)
		}
	}
	return batch
}

// tcpAnswer: ответ по TCP; без ответа по существу имя возвращается к запросам UDP.
func (e *engine) tcpAnswer(idx int32, a Answer, now time.Duration) {
	if definite(a.Kind) {
		if e.book.settle(idx, a) {
			e.progressAt = now
		}
		return
	}
	if e.book.state[idx] != viaTCP {
		return
	}
	if a.Kind == Fail {
		e.book.noteFail(idx)
	}
	e.again(idx)
}

// tcpLost: TCP у сервера n не работает - его имена возвращаются к запросам UDP.
func (e *engine) tcpLost(n int, batch []int32) {
	p := e.peers[n]
	p.tcpDown = true
	for _, idx := range append(batch, p.tcp...) {
		if e.book.state[idx] == viaTCP {
			e.again(idx)
		}
	}
	p.tcp = nil
}
