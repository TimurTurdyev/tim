package sweep

import (
	"testing"
	"time"
)

// span: за время took ушло sent запросов - ok с ответом по существу, lost пропало, clipped обрезано; ответы идут 20 мс.
type span struct {
	took                    time.Duration
	sent, ok, lost, clipped int
	// rate: частота после решения
	rate float64
}

func play(p *pacer, now time.Duration, s span) time.Duration {
	for range s.sent {
		p.onSend()
	}
	for range s.ok {
		p.onReply(now, now+20*time.Millisecond, true, false)
	}
	for range s.clipped {
		p.onReply(now, now+20*time.Millisecond, false, true)
	}
	for range s.lost {
		p.onLoss(now)
	}
	now += s.took
	p.step(now)
	return now
}

func TestPacerStep(t *testing.T) {
	zone := limits{start: startRate, floor: floorRate, top: serverTop, minTimeout: minTimeout}
	cases := []struct {
		name string
		// from, good: частота в начале и последняя чистая частота
		from, good float64
		spans      []span
	}{
		{"чистая эпоха удваивает частоту", 50, 0, []span{{600 * time.Millisecond, 30, 30, 0, 0, 100}}},
		{"разрешённая частота выбрана на 60% - расти незачем", 50, 0, []span{{time.Second, 30, 30, 0, 0, 50}}},
		{"потери выше 1%, но ниже 5%: частота держится", 50, 0, []span{
			{600 * time.Millisecond, 30, 30, 0, 0, 100},
			{time.Second, 100, 97, 3, 0, 100},
		}},
		{"выше 200/с частота держится секунду", 400, 200, []span{
			{500 * time.Millisecond, 200, 200, 0, 0, 400},
			{500 * time.Millisecond, 200, 200, 0, 0, 800},
		}},
		{"три обрезанных ответа: откат сразу, две секунды без решений, потом деление пополам", 400, 200, []span{
			{100 * time.Millisecond, 40, 30, 0, 3, 200},
			{time.Second, 200, 200, 0, 0, 200},
			{900 * time.Millisecond, 180, 180, 0, 0, 200},
			{time.Second, 200, 200, 0, 0, 300},
		}},
		{"половина запросов без ответа: частота вдвое ниже", 800, 400, []span{{200 * time.Millisecond, 160, 20, 20, 0, 400}}},
		{"потери выше 5% без чистой частоты: откат на четверть", 400, 0, []span{
			{time.Second, 400, 400, 0, 0, 800},
			{200 * time.Millisecond, 160, 20, 20, 0, 400},
			{time.Second, 400, 400, 0, 0, 400},
			{1100 * time.Millisecond, 440, 352, 88, 0, 300},
		}},
		{"потери на начальной частоте - фон: 4% расти не мешают, 20% откатывают на чистую частоту", 50, 0, []span{
			{3 * time.Second, 150, 144, 6, 0, 100},
			{time.Second, 100, 96, 4, 0, 200},
			{time.Second, 200, 160, 40, 0, 100},
		}},
		{"восемь запросов без единого ответа: сервер молчит, три ответа возвращают его на стартовую частоту", 50, 0, []span{
			{time.Second, 8, 0, 8, 0, floorRate},
			{time.Second, 3, 3, 0, 0, 50},
		}},
		{"замолчал после чистой работы: возвращается на четверть той частоты", 800, 400, []span{
			{time.Second, 8, 0, 8, 0, floorRate},
			{time.Second, 3, 3, 0, 0, 200},
		}},
	}
	for _, c := range cases {
		p := newPacer(zone, totalTop)
		p.rate, p.good = c.from, c.good
		var now time.Duration
		for i, s := range c.spans {
			if now = play(&p, now, s); p.rate != s.rate {
				t.Errorf("%s: после шага %d частота %.0f, ожидалась %.0f", c.name, i+1, p.rate, s.rate)
			}
		}
	}
}

func TestReplyTimeout(t *testing.T) {
	var d delay
	if got := d.limit(minTimeout, firstTimeout); got != firstTimeout {
		t.Errorf("без замеров срок %v, ожидался %v", got, firstTimeout)
	}
	for range 20 {
		d.sample(20 * time.Millisecond)
	}
	if got := d.limit(minTimeout, firstTimeout); got != minTimeout {
		t.Errorf("при ответах за 20 мс срок %v, ожидался %v", got, minTimeout)
	}
	if got := d.limit(minTimeoutResolver, firstTimeout); got != minTimeoutResolver {
		t.Errorf("у резолвера срок %v, ожидался %v", got, minTimeoutResolver)
	}
	d.sample(400 * time.Millisecond)
	if got := d.limit(minTimeout, firstTimeout); got <= minTimeout || got >= maxTimeout {
		t.Errorf("после ответа за 400 мс срок %v: должен вырасти, но не до потолка", got)
	}
	for range 20 {
		d.sample(5 * time.Second)
	}
	if got := d.limit(minTimeout, firstTimeout); got != maxTimeout {
		t.Errorf("при ответах за 5 с срок %v, ожидался %v", got, maxTimeout)
	}
}
