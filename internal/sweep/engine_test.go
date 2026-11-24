package sweep

import (
	"fmt"
	"math/rand/v2"
	"strings"
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

// model: учебный сервер без сокетов: limit - потолок в секунду, rrl - отказы как у BIND (долг на window секунд, каждый второй обрезан).
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
	// bigEvery: у каждого такого имени ответ не влезает в UDP и всегда приходит обрезанным
	bigEvery int

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
	// по серверам: сколько запросов пришло, частота и срок ответа в конце, молчит ли
	got     []int
	rate    []float64
	timeout []time.Duration
	silent  []bool
}

func word(i int) string { return fmt.Sprintf("w%06d.example.com", i) }

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
		names[i] = word(i)
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
	out.res = w.eng.result()
	for n, m := range w.models {
		pace := &w.eng.peers[n].pace
		out.got, out.rate = append(out.got, m.got), append(out.rate, pace.rate)
		out.timeout, out.silent = append(out.timeout, pace.timeout()), append(out.silent, pace.silent)
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
	if what == answered && m.bigEvery > 0 && s.idx != probe && int(s.idx)%m.bigEvery == m.bigEvery-1 {
		what = clipped
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

// sound: взятые имена идут подряд с начала списка, ответ каждого - про него, счёт неотвеченных честный.
func sound(t *testing.T, name string, o outcome) {
	t.Helper()
	unanswered := 0
	for i, a := range o.res.Answers {
		switch {
		case a.Name != word(i):
			t.Fatalf("%s: ответ %d про %s", name, i, a.Name)
		case a.Kind == None || a.Kind == Fail:
			unanswered++
		case (a.Kind == Addr) != live(int32(i)) || (a.Kind == NX) == live(int32(i)):
			t.Fatalf("%s: %s получил вид %d", name, a.Name, a.Kind)
		}
	}
	if unanswered != o.res.Unanswered {
		t.Errorf("%s: без ответа %d имён, а в итоге сказано %d", name, unanswered, o.res.Unanswered)
	}
}

func limited(m model, limit float64) model {
	m.limit = limit
	return m
}

// Сроки в ожиданиях - замер того же правила на сокетах плюс запас: 40 000 слов за 16.4 с без предела, 30 000 за 18.3 с при 1000/с.
func TestEngineFindsRate(t *testing.T) {
	fast := model{delay: 20 * time.Millisecond, jitter: 10 * time.Millisecond}
	lossy := func(share float64) model { return model{delay: 20 * time.Millisecond, loss: share} }
	top := [2]float64{serverTop, serverTop}
	cases := []struct {
		name   string
		models []model
		words  int
		within time.Duration
		// rate: от и до для частоты каждого сервера в конце
		rate [][2]float64
	}{
		{"без предела", []model{fast, fast}, 40000, 18 * time.Second, [][2]float64{top, top}},
		{"предел 1000/с на каждом", []model{limited(fast, 1000), limited(fast, 1000)}, 30000, 21 * time.Second, [][2]float64{{600, 1100}, {600, 1100}}},
		{"предел 300/с на одном", []model{limited(fast, 300), fast}, 30000, 22 * time.Second, [][2]float64{{200, 400}, top}},
		{"случайные потери 1%", []model{lossy(0.01), lossy(0.01)}, 30000, 15 * time.Second, [][2]float64{top, top}},
		{"случайные потери 4%", []model{lossy(0.04), lossy(0.04)}, 30000, 18 * time.Second, [][2]float64{top, top}},
	}
	for _, c := range cases {
		o := simulate(c.models, c.words, false, 0)
		sound(t, c.name, o)
		if len(o.res.Answers) != c.words || o.res.Unanswered != 0 {
			t.Errorf("%s: взято %d из %d, без ответа %d", c.name, len(o.res.Answers), c.words, o.res.Unanswered)
		}
		if o.elapsed > c.within {
			t.Errorf("%s: %v, а должно хватить %v", c.name, o.elapsed, c.within)
		}
		for n, r := range o.rate {
			if r < c.rate[n][0] || r > c.rate[n][1] {
				t.Errorf("%s: сервер %d на частоте %.0f, ожидалось от %.0f до %.0f", c.name, n, r, c.rate[n][0], c.rate[n][1])
			}
		}
		if o.res.Limited {
			t.Errorf("%s: обрезанных ответов не было, а ограничение названо", c.name)
		}
	}
}

func TestEngineSilentServer(t *testing.T) {
	fast := model{delay: 20 * time.Millisecond, jitter: 10 * time.Millisecond}
	late := model{delay: 20 * time.Millisecond, deadTill: 3 * time.Second}
	cases := []struct {
		name   string
		models []model
		budget time.Duration
		// issued: сколько имён должно быть взято, не меньше; spared: больше запросов первый сервер получить не должен
		issued int
		spared int
		silent bool
	}{
		{"молчит с начала", []model{{deadTill: forever}, fast}, 20 * time.Second, 20000, 200, true},
		{"замолкает на третьей секунде", []model{{delay: 20 * time.Millisecond, deadFrom: 3 * time.Second}, fast}, 20 * time.Second, 20000, 1100, true},
		{"отвечает отказом", []model{{delay: 20 * time.Millisecond, refuse: true}, fast}, 0, 20000, 150, true},
		// новый поток UDP первые секунды может не получить ни одного ответа: имена на этом не теряются
		{"оба молчат первые три секунды", []model{late, late}, 0, 20000, 20000, false},
	}
	for _, c := range cases {
		o := simulate(c.models, 20000, false, c.budget)
		sound(t, c.name, o)
		if len(o.res.Answers) < c.issued || o.res.Unanswered != 0 {
			t.Errorf("%s: взято %d, без ответа %d", c.name, len(o.res.Answers), o.res.Unanswered)
		}
		if o.got[0] > c.spared || o.silent[0] != c.silent {
			t.Errorf("%s: первый сервер получил %d запросов, молчащим назван: %v", c.name, o.got[0], o.silent[0])
		}
		if o.rate[1] != serverTop {
			t.Errorf("%s: второй сервер на частоте %.0f", c.name, o.rate[1])
		}
	}
}

func TestEngineAllSilent(t *testing.T) {
	dead := []model{{deadTill: forever}, {deadTill: forever}}
	for _, budget := range []time.Duration{0, 3 * time.Second} {
		o := simulate(dead, 1000, false, budget)
		sound(t, "все молчат", o)
		// без срока перебор кончается сам, когда ответов давно нет
		from, to := stallLimit, stallLimit+time.Second
		if budget > 0 {
			from, to = budget, budget
		}
		if o.elapsed < from || o.elapsed > to {
			t.Errorf("срок %v: перебор шёл %v, ожидалось от %v до %v", budget, o.elapsed, from, to)
		}
		if n := len(o.res.Answers); n == 0 || n > 200 || o.res.Unanswered != n {
			t.Errorf("срок %v: взято %d, без ответа %d - молчащим серверам имена не раздаются", budget, n, o.res.Unanswered)
		}
	}
}

func TestEngineTruncated(t *testing.T) {
	rrl := model{delay: 7 * time.Millisecond, limit: 250, rrl: true}
	closed := rrl
	closed.noTCP = true
	debt := model{delay: 20 * time.Millisecond, limit: 50, rrl: true, window: 15}
	cases := []struct {
		name   string
		models []model
		// issued: сколько имён должно быть взято, от и до; unanswered - не больше
		issued     [2]int
		unanswered int
	}{
		// отказы ограничителя - обрезанный ответ или тишина - попыток имени не съедают: оно уходит по TCP
		{"отказы уходят по TCP", []model{rrl, rrl}, [2]int{9000, 11500}, 0},
		{"TCP у сервера закрыт", []model{closed, closed}, [2]int{8500, 11000}, 100},
		{"ограничитель помнит превышение 15 с", []model{debt}, [2]int{600, 1100}, 0},
	}
	for _, c := range cases {
		o := simulate(c.models, 20000, false, 20*time.Second)
		sound(t, c.name, o)
		if n := len(o.res.Answers); n < c.issued[0] || n > c.issued[1] || o.res.Unanswered > c.unanswered {
			t.Errorf("%s: взято %d, без ответа %d", c.name, n, o.res.Unanswered)
		}
		if !o.res.Limited {
			t.Errorf("%s: ограничение частоты не названо", c.name)
		}
		// давить на ограничитель бесполезно: запросов уходит немногим больше, чем он пропускает
		for n, got := range o.got {
			if allowed := c.models[n].limit * o.elapsed.Seconds(); float64(got) > 1.3*allowed {
				t.Errorf("%s: сервер %d получил %d запросов при разрешённых %.0f", c.name, n, got, allowed)
			}
		}
	}
}

// Ответ четырёх имён не влезает в UDP: он есть только по TCP, а без TCP имя бросается после всех попыток.
func TestEngineBigAnswer(t *testing.T) {
	big := model{delay: 20 * time.Millisecond, bigEvery: 5000}
	closed := big
	closed.noTCP = true
	cases := []struct {
		name       string
		models     []model
		unanswered int
	}{
		{"TCP отвечает", []model{big, big}, 0},
		{"TCP закрыт", []model{closed, closed}, 4},
	}
	for _, c := range cases {
		o := simulate(c.models, 20000, false, 0)
		sound(t, c.name, o)
		if len(o.res.Answers) != 20000 || o.res.Unanswered != c.unanswered {
			t.Errorf("%s: взято %d, без ответа %d", c.name, len(o.res.Answers), o.res.Unanswered)
		}
		// редкий обрезанный ответ - не ограничение частоты, и ждать такое имя бесконечно незачем
		if o.res.Limited || o.elapsed > 12*time.Second {
			t.Errorf("%s: ограничение названо: %v, перебор шёл %v", c.name, o.res.Limited, o.elapsed)
		}
	}
}

func TestEngineBudget(t *testing.T) {
	fast := model{delay: 20 * time.Millisecond, jitter: 10 * time.Millisecond}
	slow := model{delay: 150 * time.Millisecond, jitter: 400 * time.Millisecond, limit: 400}
	cases := []struct {
		name      string
		models    []model
		recursive bool
		budget    time.Duration
		// issued: сколько имён должно быть взято, от и до
		issued [2]int
	}{
		{"срок 5 с при пределе 800/с", []model{limited(fast, 800)}, false, 5 * time.Second, [2]int{1500, 4000}},
		{"срок короче первого срока ответа", []model{fast, fast}, false, 800 * time.Millisecond, [2]int{20, 80}},
		{"медленный резолвер", []model{slow}, true, 20 * time.Second, [2]int{2500, 8000}},
	}
	for _, c := range cases {
		o := simulate(c.models, 100000, c.recursive, c.budget)
		sound(t, c.name, o)
		if o.elapsed > c.budget {
			t.Errorf("%s: перебор шёл %v при сроке %v", c.name, o.elapsed, c.budget)
		}
		// новые имена перестают выдаваться заранее, поэтому повторы успевают вернуться
		if n := len(o.res.Answers); n < c.issued[0] || n > c.issued[1] || o.res.Unanswered > n/100 {
			t.Errorf("%s: взято %d, без ответа %d", c.name, n, o.res.Unanswered)
		}
		if c.recursive && o.timeout[0] < minTimeoutResolver {
			t.Errorf("%s: срок ответа %v, у резолвера он не короче %v", c.name, o.timeout[0], minTimeoutResolver)
		}
	}
}

func TestEngineMatchesReply(t *testing.T) {
	e := newEngine([]string{word(0), word(1)}, 1, false, 0)
	var shots []shot
	var now time.Duration
	for len(shots) == 0 {
		now += tickEvery
		shots = e.tick(now)
	}
	s := shots[0]
	// номер тот же, имя чужое; имя своё, номер чужой
	e.receive(0, reply(s.id, word(1), 1, answered), now)
	e.receive(0, reply(s.id+7, word(0), 0, answered), now)
	if a := e.result().Answers[0]; a.Kind != None {
		t.Fatalf("чужой ответ принят: %+v", a)
	}
	e.receive(0, reply(s.id, strings.ToUpper(word(0)), 0, answered), now)
	if a := e.result().Answers[0]; a.Kind != Addr || a.Name != word(0) {
		t.Errorf("ответ с именем в другом регистре не принят: %+v", a)
	}
}
