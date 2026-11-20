package sweep

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	silence = iota
	answered
	clipped
	refused
)

const (
	// liveEvery: каждое такое имя у учебного сервера заведено, остальных нет
	liveEvery = 97
	tcpDelay  = 30 * time.Millisecond
	forever   = time.Duration(1 << 62)
)

// model: учебный сервер зоны без сокетов. limit - потолок запросов в секунду, лишнее пропадает; rrl - как
// ограничение частоты ответов в BIND: счёт уходит в минус на window секунд, каждый второй отказ обрезан.
type model struct {
	limit    float64
	rrl      bool
	window   float64
	delay    time.Duration
	jitter   time.Duration
	loss     float64
	deadFrom time.Duration
	deadTill time.Duration
	refuse   bool
	noTCP    bool

	balance float64
	last    time.Duration
	got     int
	denied  int
}

func (m *model) verdict(now time.Duration, rng *rand.Rand) int {
	m.got++
	switch {
	case now < m.deadTill || (m.deadFrom > 0 && now >= m.deadFrom):
		return silence
	case m.refuse:
		return refused
	case m.loss > 0 && rng.Float64() < m.loss:
		return silence
	case m.limit == 0:
		return answered
	}
	top := m.limit / 10
	if m.rrl {
		top = m.limit
	}
	m.balance = min(top, m.balance+m.limit*(now-m.last).Seconds())
	m.last = now
	switch {
	case m.rrl && m.balance <= 0:
		m.balance = max(m.balance-1, -m.window*m.limit)
		if m.denied++; m.denied%2 == 0 {
			return clipped
		}
		return silence
	case !m.rrl && m.balance < 1:
		return silence
	}
	m.balance--
	return answered
}

type arrival struct {
	server int
	idx    int32
	msg    []byte
	tcp    bool
}

// world: движок и учебные серверы на общих придуманных часах.
type world struct {
	eng    *engine
	models []*model
	due    map[int][]arrival
	rng    *rand.Rand
}

type outcome struct {
	res     Result
	elapsed time.Duration
	got     []int
	rate    []float64
	silent  []bool
	timeout []time.Duration
}

func live(idx int32) bool { return idx == probe || idx%liveEvery == 0 }

// reply: сообщение учебного сервера - настоящий ответ DNS, чтобы движок сам сверял номер и имя.
func reply(id uint16, name string, idx int32, what int) []byte {
	h := dnsmessage.Header{ID: id, Response: true, Authoritative: true}
	switch {
	case what == refused:
		h.RCode, h.Authoritative = dnsmessage.RCodeRefused, false
	case what == clipped:
		h.RCode, h.Truncated = dnsmessage.RCodeNameError, true
	case !live(idx):
		h.RCode = dnsmessage.RCodeNameError
	}
	q := dnsmessage.Question{Name: dnsmessage.MustNewName(name + "."), Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}
	b := dnsmessage.NewBuilder(make([]byte, 0, 96), h)
	_ = b.StartQuestions()
	_ = b.Question(q)
	if what == answered && live(idx) {
		_ = b.StartAnswers()
		_ = b.AResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 300}, dnsmessage.AResource{A: [4]byte{192, 0, 2, 10}})
	}
	msg, _ := b.Finish()
	return msg
}

func simulate(models []model, words int, recursive bool, budget time.Duration) outcome {
	names := make([]string, words)
	for i := range names {
		names[i] = fmt.Sprintf("w%06d.example.com", i)
	}
	w := &world{eng: newEngine(names, len(models), recursive, budget), due: map[int][]arrival{}, rng: rand.New(rand.NewPCG(7, 11))}
	for i := range models {
		m := models[i]
		if m.balance = m.limit / 10; m.rrl {
			m.balance = m.limit
		}
		w.models = append(w.models, &m)
	}
	out := outcome{elapsed: w.run()}
	out.res = w.eng.book.result()
	for n, m := range w.models {
		pace := &w.eng.peers[n].pace
		out.got, out.rate = append(out.got, m.got), append(out.rate, pace.rate)
		out.silent, out.timeout = append(out.silent, pace.silent), append(out.timeout, pace.timeout())
	}
	return out
}

func (w *world) run() time.Duration {
	for step := 0; ; step++ {
		now := time.Duration(step) * tickEvery
		for _, a := range w.due[step] {
			w.deliver(a, now)
		}
		delete(w.due, step)
		for _, s := range w.eng.tick(now) {
			w.ask(s, now, step)
		}
		for n := range w.models {
			w.overTCP(n, step)
		}
		if w.eng.finished(now) {
			return now
		}
	}
}

func (w *world) ask(s shot, now time.Duration, step int) {
	m := w.models[s.server]
	what := m.verdict(now, w.rng)
	if what == silence {
		return
	}
	wait := m.delay
	if m.jitter > 0 {
		wait += time.Duration(w.rng.Int64N(int64(m.jitter)))
	}
	at := step + max(1, int(wait/tickEvery))
	w.due[at] = append(w.due[at], arrival{server: s.server, idx: s.idx, msg: reply(s.id, w.eng.name(s.idx), s.idx, what)})
}

// overTCP: TCP ограничения частоты не знает - ответ на каждый вопрос; у сервера без TCP очередь закрывается.
func (w *world) overTCP(n, step int) {
	batch := w.eng.takeTCP(n, tcpWindow)
	if len(batch) == 0 {
		return
	}
	if w.models[n].noTCP {
		w.eng.tcpLost(n, batch)
		return
	}
	at := step + int(tcpDelay/tickEvery)
	for _, idx := range batch {
		w.due[at] = append(w.due[at], arrival{server: n, idx: idx, msg: reply(0, w.eng.name(idx), idx, answered), tcp: true})
	}
}

func (w *world) deliver(a arrival, now time.Duration) {
	if !a.tcp {
		w.eng.receive(a.server, a.msg, now)
		return
	}
	answer, _ := Classify(a.msg, w.eng.name(a.idx))
	w.eng.tcpAnswer(a.idx, answer, now)
}

func TestDiag(t *testing.T) {
	fast := model{delay: 20 * time.Millisecond, jitter: 10 * time.Millisecond}
	lim := func(l float64) model { m := fast; m.limit = l; return m }
	type sc struct {
		name   string
		models []model
		words  int
		budget time.Duration
		rec    bool
	}
	scs := []sc{
		{"A no limit x2", []model{fast, fast}, 40000, 0, false},
		{"B limit 1000 x2", []model{lim(1000), lim(1000)}, 30000, 0, false},
		{"C dead + fast", []model{{deadTill: forever}, fast}, 30000, 20 * time.Second, false},
		{"D dies at 3s + fast", []model{{delay: 20 * time.Millisecond, deadFrom: 3 * time.Second}, fast}, 40000, 20 * time.Second, false},
		{"E loss 1%", []model{{delay: 20 * time.Millisecond, loss: 0.01}, {delay: 20 * time.Millisecond, loss: 0.01}}, 30000, 0, false},
		{"F 300 + fast", []model{lim(300), fast}, 30000, 0, false},
		{"G RRL 50 window 15", []model{{delay: 20 * time.Millisecond, limit: 50, rrl: true, window: 15}}, 30000, 20 * time.Second, false},
		{"G2 RRL 50 window 15 noTCP", []model{{delay: 20 * time.Millisecond, limit: 50, rrl: true, window: 15, noTCP: true}}, 30000, 20 * time.Second, false},
		{"H slow resolver", []model{{delay: 150 * time.Millisecond, jitter: 400 * time.Millisecond, limit: 400}}, 20000, 20 * time.Second, true},
		{"I budget 5s limit 800", []model{lim(800)}, 100000, 5 * time.Second, false},
		{"J refused + fast", []model{{delay: 20 * time.Millisecond, refuse: true}, fast}, 20000, 0, false},
		{"N RRL 250 x2", []model{{delay: 7 * time.Millisecond, limit: 250, rrl: true}, {delay: 7 * time.Millisecond, limit: 250, rrl: true}}, 20000, 20 * time.Second, false},
		{"N2 RRL 250 x2 noTCP", []model{{delay: 7 * time.Millisecond, limit: 250, rrl: true, noTCP: true}, {delay: 7 * time.Millisecond, limit: 250, rrl: true, noTCP: true}}, 20000, 20 * time.Second, false},
		{"N3 RRL 250 x2 window 5", []model{{delay: 7 * time.Millisecond, limit: 250, rrl: true, window: 5}, {delay: 7 * time.Millisecond, limit: 250, rrl: true, window: 5}}, 20000, 40 * time.Second, false},
		{"K loss 4%", []model{{delay: 20 * time.Millisecond, loss: 0.04}, {delay: 20 * time.Millisecond, loss: 0.04}}, 30000, 0, false},
		{"L both dead 3s", []model{{delay: 20 * time.Millisecond, deadTill: 3 * time.Second}, {delay: 20 * time.Millisecond, deadTill: 3 * time.Second}}, 30000, 0, false},
		{"M all dead", []model{{deadTill: forever}, {deadTill: forever}}, 1000, 0, false},
		{"O tiny budget", []model{fast, fast}, 1000, 800 * time.Millisecond, false},
	}
	for _, s := range scs {
		start := time.Now()
		o := simulate(s.models, s.words, s.rec, s.budget)
		wrong := 0
		for i, a := range o.res.Answers {
			if a.Kind != None && (a.Kind == Addr) != live(int32(i)) {
				wrong++
			}
		}
		t.Logf("%-26s took %-6s issued %6d unanswered %5d wrong %d limited %-5v got %v rate %.0f silent %v timeout %v (wall %s)", s.name, o.elapsed.Round(100*time.Millisecond),
			len(o.res.Answers), o.res.Unanswered, wrong, o.res.Limited, o.got, o.rate, o.silent, o.timeout, time.Since(start).Round(time.Millisecond))
	}
}
