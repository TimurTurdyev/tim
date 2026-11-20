package sweep

import (
	"math"
	"time"
)

const (
	startRate         = 50
	startRateResolver = 20
	// floorRate: с такой частотой молчащий сервер спрашивают, жив ли он
	floorRate = 5
	serverTop = 1500
	totalTop  = 3000
	// burst: запас жетонов в долях секунды - сглаживает неровный шаг движка
	burst = 0.02

	// fastUntil: выше этой частоты эпоха не короче секунды - ограничители считают по секундам
	fastUntil = 200
	fastEpoch = 400 * time.Millisecond
	slowEpoch = time.Second
	maxEpoch  = 4 * time.Second

	growUnder = 0.01
	cutOver   = 0.05
	halveOver = 0.5
	// usedEnough: расти есть смысл, только если разрешённая частота выбрана хотя бы на столько
	usedEnough = 0.7
	minSample  = 30
	// maxBase: больше таких потерь на начальной частоте фоном считать нельзя
	maxBase    = 0.3
	baseSample = 150
	baseWait   = 8 * time.Second

	truncatedCut = 3
	// holdAfterCut: ограничитель ещё наказывает за прошлую частоту - новые решения уронили бы её каскадом
	holdAfterCut = 2 * time.Second
	// probeAfter: столько чистых эпох на найденной частоте - и проба выше: ограничение могли снять
	probeAfter    = 8
	probeAfterMax = 64
	probeStep     = 1.15

	silentAfter = 8
	wakeAfter   = 3
	wakeWindow  = 2 * time.Second

	firstTimeout       = time.Second
	minTimeout         = 200 * time.Millisecond
	minTimeoutResolver = 1500 * time.Millisecond
	maxTimeout         = 3 * time.Second
)

// delay: оценка времени ответа - сглаженное среднее и разброс, как срок повтора в TCP.
type delay struct{ mean, spread time.Duration }

func (d *delay) sample(took time.Duration) {
	took = max(took, time.Microsecond)
	if d.mean == 0 {
		d.mean, d.spread = took, took/2
		return
	}
	d.spread = (3*d.spread + (d.mean - took).Abs()) / 4
	d.mean = (7*d.mean + took) / 8
}

// limit: срок ожидания ответа; first - пока замеров нет.
func (d *delay) limit(least, first time.Duration) time.Duration {
	if d.mean == 0 {
		return max(first, least)
	}
	return min(max(d.mean+4*d.spread, least), maxTimeout)
}

type limits struct {
	start, floor, top float64
	minTimeout        time.Duration
}

// pacer: частота одного сервера - разгон вдвое до первых потерь, дальше деление пополам между good и bad.
type pacer struct {
	delay
	lim          limits
	rate, tokens float64
	// good - частота, которая прошла чисто; bad - на которой начались потери
	good, bad float64
	// base: потери пути на частоте baseRate; решения принимаются по превышению над ними
	base, baseRate float64
	based          bool
	// since: попытки, отправленные раньше, в счёт эпохи не идут - потери старой частоты не роняют новую
	since   time.Duration
	hold    time.Duration
	epochAt time.Duration
	// итоги эпохи: ответы по существу, потери и отказы, из них обрезанные, отправлено
	ok, lost, truncated, sent int
	calm, wait                int
	probing                   bool
	// silent: сервер не отвечает - имён ему не дают, идут только пробы
	silent bool
}

func newPacer(lim limits, share float64) pacer {
	return pacer{lim: lim, rate: min(lim.start, lim.top, share), wait: probeAfter}
}

func (p *pacer) timeout() time.Duration { return p.limit(p.lim.minTimeout, firstTimeout) }

// epoch: сколько держать частоту до решения о росте.
func (p *pacer) epoch() time.Duration {
	least := fastEpoch
	if p.bad > 0 || p.rate >= fastUntil {
		least = slowEpoch
	}
	return min(max(2*p.timeout(), least), maxEpoch)
}

func (p *pacer) refill(seconds float64) {
	p.tokens = min(p.tokens+p.rate*seconds, p.rate*burst+1)
}

func (p *pacer) onSend() {
	p.sent++
	p.tokens--
}

// onReply: ответ в срок. Отказ и обрезанный ответ - потеря; в оценку срока они не идут: резолвер шлёт их через секунды.
func (p *pacer) onReply(sentAt, now time.Duration, good, truncated bool) {
	if good {
		p.sample(now - sentAt)
	}
	switch {
	case sentAt < p.since:
	case good:
		p.ok++
	default:
		p.lost++
		if truncated {
			p.truncated++
		}
	}
}

// onLate: ответ пришёл после срока - потеря была мнимой.
func (p *pacer) onLate(sentAt, now time.Duration) {
	p.sample(now - sentAt)
	if sentAt >= p.since && p.lost > 0 {
		p.lost--
		p.ok++
	}
}

func (p *pacer) onLoss(sentAt time.Duration) {
	if sentAt >= p.since {
		p.lost++
	}
}

// step: решение о частоте по итогам эпохи; откат конца эпохи не ждёт.
func (p *pacer) step(now time.Duration) {
	age, n := now-p.epochAt, p.ok+p.lost
	if p.ok == 0 && p.lost >= silentAfter {
		p.mute(now)
		return
	}
	if p.silent {
		p.wake(now, age)
		return
	}
	if n == 0 {
		if age > 3*p.epoch() {
			p.reset(now)
		}
		return
	}
	if now < p.hold {
		if age >= p.epoch() {
			p.reset(now)
		}
		return
	}
	if p.overloaded(now, n) || age < p.epoch() || (n < minSample && age < 3*p.epoch()) {
		return
	}
	if p.calibrated(n, age) {
		p.grow(now, n, age)
	}
}

func (p *pacer) reset(now time.Duration) {
	p.ok, p.lost, p.truncated, p.sent, p.epochAt = 0, 0, 0, 0, now
}

func (p *pacer) mute(now time.Duration) {
	if !p.silent {
		// молчание после чистой работы - возможно, наказание за частоту; молчание с начала о ней не говорит
		if p.good > 0 && (p.bad == 0 || p.rate < p.bad) {
			p.bad = p.rate
		}
		p.good = 0
	}
	p.rate, p.silent, p.probing, p.calm, p.since = p.lim.floor, true, false, 0, now
	p.reset(now)
}

func (p *pacer) wake(now, age time.Duration) {
	switch {
	case p.ok >= wakeAfter:
		p.silent, p.based, p.since = false, false, now
		p.rate = min(p.lim.start, p.lim.top)
		if p.bad > 0 {
			p.rate = min(p.lim.top, max(p.lim.floor, p.bad/4))
		}
		p.reset(now)
	case age > wakeWindow:
		p.reset(now)
	}
}

// overloaded: потерь столько, что частоту надо сбросить сейчас - меньше, чем кажется, их уже не станет.
func (p *pacer) overloaded(now time.Duration, n int) bool {
	switch {
	case n >= minSample && float64(p.lost) >= halveOver*float64(n):
		p.good = 0
		p.cut(now, p.rate/2)
	case p.truncated >= truncatedCut:
		p.cut(now, p.retreat(0.5))
	case p.based && n >= minSample && above(p.lost, n, p.base+cutOver, 2):
		p.cut(now, p.retreat(0.75))
	default:
		return false
	}
	return true
}

// retreat: куда откатиться - на последнюю чистую частоту, а если её нет, то в share раз ниже.
func (p *pacer) retreat(share float64) float64 {
	if p.good > 0 && p.good < p.rate {
		return p.good
	}
	p.good = 0
	return p.rate * share
}

func (p *pacer) cut(now time.Duration, to float64) {
	old := p.rate
	if p.bad == 0 || old < p.bad {
		p.bad = old
	}
	p.rate = max(p.lim.floor, to)
	if p.good >= old {
		p.good = 0
	}
	if p.probing {
		p.wait = min(p.wait*2, probeAfterMax)
	}
	// ушли ниже частоты, на которой мерили фон: его надо мерить заново
	if p.rate < 0.9*p.baseRate {
		p.based = false
	}
	p.probing, p.calm = false, 0
	p.since, p.hold = now, now+holdAfterCut
	p.reset(now)
}

// calibrated: фон потерь измерен; без потерь в первой эпохе он ноль, иначе копится выборка побольше.
func (p *pacer) calibrated(n int, age time.Duration) bool {
	if p.based {
		return true
	}
	if p.lost > 0 && n < baseSample && age < baseWait {
		return false
	}
	p.based, p.base, p.baseRate = true, min(float64(p.lost)/float64(n), maxBase), p.rate
	return true
}

func (p *pacer) grow(now time.Duration, n int, age time.Duration) {
	used := float64(p.sent) / (p.rate * age.Seconds())
	if above(p.lost, n, p.base+growUnder, 1) || used < usedEnough {
		p.reset(now)
		return
	}
	p.good = max(p.good, p.rate)
	if p.probing {
		p.probing, p.wait = false, probeAfter
	}
	switch {
	case p.bad == 0:
		p.rate = min(p.lim.top, p.rate*2)
	case p.bad-p.good > max(0.1*p.bad, 2):
		p.rate = min(p.lim.top, (p.good+p.bad)/2)
	default:
		p.plateau()
	}
	p.reset(now)
}

// plateau: частота найдена - держимся и изредка пробуем выше.
func (p *pacer) plateau() {
	p.calm++
	if p.calm < p.wait || p.good >= p.lim.top {
		return
	}
	p.calm, p.probing = 0, true
	p.bad = p.good * 1.3
	p.rate = min(p.lim.top, p.good*probeStep)
}

// above: потерь значимо больше доли share; запас в sigmas отклонений - от случайного разброса малой выборки.
func above(lost, n int, share, sigmas float64) bool {
	share = min(max(share, 0.005), 0.95)
	return float64(lost) > float64(n)*share+sigmas*math.Sqrt(float64(n)*share*(1-share))
}
